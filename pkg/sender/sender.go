package sender

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/metrics"
)

var (
	URL          = flag.String("vm.url", "http://localhost:8428", "http endpoint for victoriametrics import api.")
	pushInterval = flag.Duration("vm.pushInterval", 30*time.Second, "interval between metrics pushes to victoria metrics")
)

var (
	pushRequestDuration = metrics.NewHistogram(`cybera_exporter_push_request_duration_seconds`)
)

const vmImportSuffix = "/api/v1/import/prometheus"

type VMClient struct {
	cl  *http.Client
	url string
	wg  *sync.WaitGroup
	// guards data buf
	mu      sync.Mutex
	dataBuf *bytesutil.ByteBuffer
}

func NewVMClient(url string, wg *sync.WaitGroup) *VMClient {
	cl := http.DefaultClient
	return &VMClient{
		cl:      cl,
		url:     url,
		wg:      wg,
		dataBuf: &bytesutil.ByteBuffer{},
	}
}

func (vmC *VMClient) WriteBufferedItems(w io.Writer) {
	vmC.mu.Lock()
	w.Write(vmC.dataBuf.B)
	vmC.mu.Unlock()
}

// StartSender starts background sender with given init data
func (vmC *VMClient) StartSender(ctx context.Context, initData []byte, data <-chan []string) error {
	t := time.NewTicker(*pushInterval)
	f := func(data []byte) error {
		t := time.Now()
		if len(data) > 0 {
			if err := vmC.sendRemote(ctx, data); err != nil {
				logger.Errorf("cannot write metrics to remote storage: %v", err)
			}
			pushRequestDuration.UpdateDuration(t)
		}
		return nil
	}
	if err := f(initData); err != nil {
		return err
	}
	go func() {
		defer vmC.wg.Done()
		for {
			select {
			case <-ctx.Done():
				logger.Infof("stopped victoria metrics sender")
				return
			case <-t.C:
				vmC.mu.Lock()
				if err := f(vmC.dataBuf.B); err != nil {
					logger.Errorf("%v", err)
				}
				vmC.mu.Unlock()
			case ts := <-data:
				vmC.mu.Lock()
				vmC.dataBuf.Reset()
				for i := range ts {
					vmC.dataBuf.Write([]byte(ts[i]))
				}
				vmC.mu.Unlock()
			}
		}
	}()
	return nil
}

func (vmC *VMClient) sendRemote(ctx context.Context, data []byte) error {
	bb := bytes.NewBuffer(data)
	req, err := http.NewRequestWithContext(ctx, "POST", vmC.url, bb)
	if err != nil {
		return err
	}
	req.URL.Path += vmImportSuffix
	resp, err := vmC.cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("unexpected status code recevied from victoriametrics, code: %d", resp.StatusCode)
	}
	return nil
}
