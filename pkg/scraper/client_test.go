package scraper

import (
	"reflect"
	"testing"
)

func Test_convertToTimeseries(t *testing.T) {
	type args struct {
		dst  []string
		data []*siteItem
		ct   int64
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertToTimeseries(tt.args.dst, tt.args.data, tt.args.ct); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertToTimeseries() = %v, want %v", got, tt.want)
			}
		})
	}
}
