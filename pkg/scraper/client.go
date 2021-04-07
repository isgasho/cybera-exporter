package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/metrics"
	"github.com/mmcloughlin/geohash"
)

var (
	URL               = flag.String("cybera.url", "", "url for accessing cybera api endpoint, for example https://cybera.net")
	cyberaUsername    = flag.String("cybera.username", "", "username for accessing cybera cloud api")
	cyberaPassword    = flag.String("cybera.password", "", "password for accessing cybera cloud api")
	scrapeInterval    = flag.Duration("cybera.scrapeInterval", 20*time.Second, "scrape interval for refreshing site items")
	scrapeConcurrency = flag.Int("cybera.scrapeConcurrency", 100, "how many concurrent inserts execute for retrieving information about site ids.")
	scrappedItems     = metrics.NewCounter(`cybera_exporter_scrapped_targets`)
	scrapeDuration    = metrics.NewHistogram(`cybera_exporter_scrape_duration_seconds`)
)

const (
	cyberaSiteAPISuffixAll = "/api/vip/site/detail/all"
	cyberaSiteAPISuffixID  = "/api/vip/site/detail/"
	cyberaSiteIDsAll       = "/api/vip/site/status/simple/all"
	authSuffix             = "/api/vip/token"
)

type CyberaClient struct {
	cl    *http.Client
	url   string
	creds []byte
	// guards token
	tokenLock     sync.RWMutex
	token         string
	tokenExpireAt time.Time
	wg            *sync.WaitGroup
}

// NewCyberaClient returns new cybera cloud api client.
func NewCyberaClient(url string, wg *sync.WaitGroup) (*CyberaClient, error) {
	cl := http.DefaultClient
	ap, err := createAuthParams()
	if err != nil {
		return nil, err
	}
	return &CyberaClient{
		cl:    cl,
		url:   url,
		wg:    wg,
		creds: ap,
	}, nil
}

func createAuthParams() ([]byte, error) {
	ap := authParams{
		Username: *cyberaUsername,
		Password: *cyberaPassword,
	}
	if err := ap.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(ap)
}

// StartScraper starts data scraper with given write channel.
func (cc *CyberaClient) StartScraper(ctx context.Context, data chan<- []string) error {
	t := time.NewTicker(*scrapeInterval)
	var scrapeBuf []string
	var err error
	f := func() error {
		defer scrapeDuration.UpdateDuration(time.Now())
		scrapeBuf, err = cc.scrapeWithLimit(ctx, scrapeBuf, *scrapeConcurrency)
		if err != nil {
			return fmt.Errorf("cannot scrape data from cybera api: %v", err)
		}
		scrappedItems.Add(len(scrapeBuf))
		if len(scrapeBuf) > 0 {
			data <- scrapeBuf
		}
		return nil
	}
	if err := f(); err != nil {
		return err
	}
	go func() {
		defer cc.wg.Done()
		for {
			select {
			case <-ctx.Done():
				logger.Infof("stopped cybera scraper")
				return
			case <-t.C:
				if err := f(); err != nil {
					logger.Errorf("%v", err)
				}
			}

		}
	}()
	return nil
}

func (cc *CyberaClient) scrapeWithLimit(ctx context.Context, dst []string, concurrency int) ([]string, error) {
	limiter := make(chan struct{}, concurrency)
	newIDs, err := cc.getSiteIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch site IDs from cybera api, err: %w", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	sitesByIDs := make([]*siteItem, 0)

	for i := range newIDs {
		id := newIDs[i]
		limiter <- struct{}{}
		wg.Add(1)
		// ignores bad response
		// probably it can lead to incomplete data response.
		// need to add threshold or return on first error.
		go func(id int64) {
			defer func() {
				<-limiter
				wg.Done()
			}()
			idInfo, err := cc.makeAPIRequest(ctx, fmt.Sprintf("%s/%d", cyberaSiteAPISuffixID, id))
			if err != nil {
				logger.Errorf("cannot query cybera details: %v", err)
				return
			}
			siteByID, err := parseIDAPIResponse(idInfo)
			if err != nil {
				logger.Errorf("cannot parse siteID response id: %d, err: %v", id, err)
				return
			}
			mu.Lock()
			sitesByIDs = append(sitesByIDs, siteByID)
			mu.Unlock()
		}(id)

	}
	wg.Wait()
	t := time.Now().UnixNano() / 1e6
	return convertToTimeseries(dst, sitesByIDs, t), nil
}

func (cc *CyberaClient) getSiteIDs(ctx context.Context) ([]int64, error) {
	data, err := cc.makeAPIRequest(ctx, cyberaSiteIDsAll)
	if err != nil {
		if errors.Is(err, unAuthorizedAccess) {
			// hack for token refresh
			cc.tokenLock.Lock()
			cc.token = ""
			cc.tokenLock.Unlock()
		}
		return nil, err
	}
	return parseIDsResponseByStatus(data)
}

func (cc *CyberaClient) scrape(ctx context.Context, dst []string) ([]string, error) {
	dst = dst[:0]
	data, err := cc.makeAPIRequest(ctx, cyberaSiteAPISuffixAll)
	if err != nil {
		return nil, err
	}
	ct := time.Now().UnixNano() / 1e6
	siteItems, err := parseAPIResponse(data)
	return convertToTimeseries(dst, siteItems, ct), nil
}

func (cc *CyberaClient) getAuthToken(ctx context.Context) (string, error) {
	cc.tokenLock.Lock()
	defer cc.tokenLock.Unlock()
	if time.Until(cc.tokenExpireAt) > 10*time.Second && cc.token != "" {
		// need to refresh token
		return cc.token, nil
	}
	if err := cc.refreshCredentials(ctx); err != nil {
		return "", err
	}
	return cc.token, nil
}

func (cc *CyberaClient) refreshCredentials(ctx context.Context) error {
	newToken, err := cc.makeAuthAPIRequest(ctx, authSuffix, cc.creds)
	if err != nil {
		return err
	}
	expT, err := getExpireTimeFromJWT(newToken)
	if err != nil {
		return err
	}
	cc.token = string(newToken)
	cc.tokenExpireAt = *expT
	return nil
}

var unAuthorizedAccess = fmt.Errorf("invalid or expired auth token")

func (cc *CyberaClient) makeAPIRequest(ctx context.Context, suffix string) ([]byte, error) {
	authToken, err := cc.getAuthToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", cc.url, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create request for api request, err: %w", err)
	}
	req.URL.Path += suffix
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := cc.cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot query api endpoint: %q, err: %w", req.URL.Redacted(), err)
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read api response, err: %w", err)
	}
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if resp.StatusCode == 403 {
		return nil, unAuthorizedAccess
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected api response status code: %d, api resp: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// retrieves credentials from cybera cloud api with given auth config.
func (cc *CyberaClient) makeAuthAPIRequest(ctx context.Context, suffix string, ac []byte) ([]byte, error) {
	bb := bytes.NewBuffer(ac)
	req, err := http.NewRequestWithContext(ctx, "POST", cc.url, bb)
	if err != nil {
		return nil, fmt.Errorf("cannot create auth request, err: %w", err)
	}
	req.URL.Path += suffix
	req.Header.Set("Content-Type", "application/json")
	resp, err := cc.cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot query auth api endpoint: %q, err: %w", req.URL.Redacted(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected auth api response status code: %d", resp.StatusCode)
	}
	token := resp.Header.Get("Authorization")
	if token == "" {
		return nil, fmt.Errorf("")
	}
	return []byte(token), nil
}

var siteStatusEnum = map[string]int8{
	"ONLINE":   0,
	"PENDING":  1,
	"OFFLINE":  2,
	"ONBACKUP": 3,
}

// convertToTimeseries converts site item to prometheus line metrics.
func convertToTimeseries(dst []string, data []*siteItem, ct int64) []string {
	dst = dst[:0]
	var bb bytesutil.ByteBuffer
	bb.Reset()
	for i := range data {
		item := data[i]
		fmt.Fprintf(&bb, "vm_cybera_site_info{")
		fmt.Fprintf(&bb, "name=%q,", item.Name)
		fmt.Fprintf(&bb, "status=%q,", item.Status)
		fmt.Fprintf(&bb, `status_id="%d",`, siteStatusEnum[item.Status])
		fmt.Fprintf(&bb, "city=%q,", item.PhysicalAddress.City)
		fmt.Fprintf(&bb, "country=%q,", item.PhysicalAddress.Country)
		fmt.Fprintf(&bb, "state=%q,", item.PhysicalAddress.State)
		fmt.Fprintf(&bb, `longitude="%.2f",`, item.PhysicalAddress.Lng)
		fmt.Fprintf(&bb, `latitude="%.2f",`, item.PhysicalAddress.Lat)
		fmt.Fprintf(&bb, "geohash=%q", geohash.Encode(item.PhysicalAddress.Lat, item.PhysicalAddress.Lng))
		fmt.Fprintf(&bb, "} 1 %d\n", ct)
		dst = append(dst, string(bb.B))
		bb.Reset()
	}
	return dst
}
