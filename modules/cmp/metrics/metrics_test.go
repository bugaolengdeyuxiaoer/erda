// Copyright (c) 2021 Terminus, Inc.
//
// This program is free software: you can use, redistribute, and/or modify
// it under the terms of the GNU Affero General Public License, version 3
// or later ("AGPL"), as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package metrics

import (
	"context"
	"reflect"
	"testing"

	"github.com/erda-project/erda-proto-go/core/monitor/metric/pb"
	"github.com/erda-project/erda/apistructs"
	"github.com/erda-project/erda/modules/cmp/cache"
)


func TestMetric_DoQuery(t *testing.T) {
	type fields struct {
		Cache   *cache.Cache
		Metricq pb.MetricServiceServer
	}
	type args struct {
		ctx context.Context
		req apistructs.MetricsRequest
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *pb.QueryWithInfluxFormatResponse
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Metric{
				Cache:   tt.fields.Cache,
				Metricq: tt.fields.Metricq,
			}
			got, err := m.DoQuery(tt.args.ctx, tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DoQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DoQuery() got = %v, want %v", got, tt.want)
			}
		})
	}
}
