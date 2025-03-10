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

package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/bluele/gcache"

	"github.com/erda-project/erda-infra/base/logs"
	"github.com/erda-project/erda-infra/base/servicehub"
	writer "github.com/erda-project/erda-infra/pkg/parallel-writer"
	"github.com/erda-project/erda-infra/providers/cassandra"
	mutex "github.com/erda-project/erda-infra/providers/etcd-mutex"
	"github.com/erda-project/erda-infra/providers/kafka"
	"github.com/erda-project/erda-infra/providers/mysql"
	"github.com/erda-project/erda/modules/core/monitor/log/schema"
)

const selector = "log-store"

type define struct{}

func (d *define) Services() []string { return []string{selector} }
func (d *define) Dependencies() []string {
	return []string{"kafka", "cassandra", "mysql", "etcd-mutex"}
}
func (d *define) Summary() string     { return "logs store" }
func (d *define) Description() string { return d.Summary() }
func (d *define) Config() interface{} { return &config{} }
func (d *define) Creator() servicehub.Creator {
	return func() servicehub.Provider {
		return &provider{}
	}
}

type config struct {
	Input  kafka.ConsumerConfig `file:"input"`
	Output struct {
		LogSchema struct {
			OrgRefreshInterval time.Duration `file:"org_refresh_interval" default:"2m" env:"LOG_SCHEMA_ORG_REFRESH_INTERVAL"`
		} `file:"log_schema"`
		Cassandra struct {
			cassandra.WriterConfig  `file:"writer_config"`
			cassandra.SessionConfig `file:"session_config"`
			GCGraceSeconds          int           `file:"gc_grace_seconds" default:"86400"`
			DefaultTTL              time.Duration `file:"default_ttl" default:"168h"`
			TTLReloadInterval       time.Duration `file:"ttl_reload_interval" default:"3m"`
			CacheStoreInterval      time.Duration `file:"cache_store_interval" default:"3m"`
		} `file:"cassandra"`
		IDKeys []string `file:"id_keys"`
	} `file:"output"`
}

type provider struct {
	Cfg          *config
	Log          logs.Logger
	Mysql        mysql.Interface
	Kafka        kafka.Interface
	EtcdMutexInf mutex.Interface
	output       writer.Writer
	ttl          ttlStore
	schema       schema.LogSchema
	cache        gcache.Cache
}

func (p *provider) Init(ctx servicehub.Context) error {
	cass := ctx.Service("cassandra").(cassandra.Interface)
	session, err := cass.Session(&p.Cfg.Output.Cassandra.SessionConfig)
	if err != nil {
		return fmt.Errorf("fail to create cassandra session, err=%s", err)
	}

	p.output = cass.NewBatchWriter(session, &p.Cfg.Output.Cassandra.WriterConfig, p.createLogStatementBuilder)

	p.ttl = &mysqlStore{
		ttlValue:      make(map[string]int),
		defaultTTLSec: int(p.Cfg.Output.Cassandra.DefaultTTL.Seconds()),
		mysql:         p.Mysql.DB(),
		Log:           p.Log.Sub("ttlStore"),
	}

	p.schema, err = schema.NewCassandraSchema(cass, p.Log.Sub("logSchema"))
	if err != nil {
		return err
	}

	p.cache = gcache.New(128).LRU().Build()

	return nil
}

func (p *provider) Run(ctx context.Context) error {
	if err := p.schema.CreateDefault(); err != nil {
		return fmt.Errorf("create default error: %w", err)
	}
	go p.schema.RunDaemon(ctx, p.Cfg.Output.LogSchema.OrgRefreshInterval, p.EtcdMutexInf)

	go p.ttl.Run(ctx, p.Cfg.Output.Cassandra.TTLReloadInterval)
	go p.startStoreMetaCache(ctx)
	if err := p.Kafka.NewConsumer(&p.Cfg.Input, p.invoke); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
	}
	return nil
}

func init() {
	servicehub.RegisterProvider(selector, &define{})
}
