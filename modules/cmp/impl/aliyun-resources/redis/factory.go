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

package redis

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/erda-project/erda/apistructs"
	"github.com/erda-project/erda/modules/cmp/dbclient"
	aliyun_resources "github.com/erda-project/erda/modules/cmp/impl/aliyun-resources"
	resource_factory "github.com/erda-project/erda/modules/cmp/impl/resource-factory"
	"github.com/erda-project/erda/pkg/crypto/uuid"
)

type RedisFactory struct {
	*resource_factory.BaseResourceFactory
}

func creator(ctx aliyun_resources.Context, m resource_factory.BaseResourceMaterial, r *dbclient.Record, d *apistructs.CreateCloudResourceRecord, v apistructs.CloudResourceVpcBaseInfo) (*apistructs.AddonConfigCallBackResponse, *dbclient.ResourceRouting, error) {
	var err error

	req, ok := m.(apistructs.CreateCloudResourceRedisRequest)
	if !ok {
		return nil, nil, errors.Errorf("convert material failed, material: %+v", m)
	}
	regionids := aliyun_resources.ActiveRegionIDs(ctx)
	list, err := List(ctx, aliyun_resources.DefaultPageOption, regionids.ECS, "")
	if err != nil {
		err = errors.Wrap(err, "list redis failed")
		return nil, nil, err
	}
	for _, item := range list {
		if req.InstanceName == item.InstanceName {
			err := errors.Errorf("redis instance already exist, region:%s, name:%s", item.RegionId, item.InstanceName)
			return nil, nil, err
		}
	}

	// auto generate password if not provide
	if req.Password == "" {
		req.Password = uuid.UUID()[:8] + "r@1" + uuid.UUID()[:8]
	}

	// auto generate AutoRenewPeriod by ChargePeriod
	if strings.ToLower(req.ChargeType) == aliyun_resources.ChargeTypePrepaid {
		p, err := strconv.Atoi(req.ChargePeriod)
		if err != nil {
			return nil, nil, errors.New("invalid charge period, support format:1-9，12，24，36, (month)")
		}
		if p >= 12 {
			req.AutoRenewPeriod = "12"
		} else if p >= 6 {
			req.AutoRenewPeriod = "6"
		} else if p >= 3 {
			req.AutoRenewPeriod = "3"
		} else if p <= 0 {
			req.AutoRenewPeriod = "1"
		}
	}

	// get available vswitch/zone
	ctx.VpcID = req.VpcID
	vsw, err := GetAvailableVsw(ctx, apistructs.CreateCloudResourceBaseInfo{VSwitchID: req.VSwitchID, ZoneID: req.ZoneID})
	if err != nil {
		logrus.Errorf("get available vswitch failed, error: %v", err)
		return nil, nil, err
	}
	req.VSwitchID = vsw.VSwitchId
	req.ZoneID = vsw.ZoneId

	logrus.Infof("start to create redis instance, request: %+v", req)
	resp, err := CreateInstance(ctx, req)
	if err != nil {
		return nil, nil, errors.Wrap(err, "create redis instance failed")
	}
	d.InstanceID = resp.InstanceId

	if req.Source != apistructs.CloudResourceSourceAddon {
		return nil, nil, nil
	}

	cbResp := &apistructs.AddonConfigCallBackResponse{
		Config: []apistructs.AddonConfigCallBackItemResponse{
			{
				Name:  "REDIS_HOST",
				Value: resp.ConnectionDomain,
			},
			{
				Name:  "REDIS_PORT",
				Value: resp.Port,
			},
			{
				Name:  "REDIS_PASSWORD",
				Value: req.Password,
			},
		},
	}

	routing := &dbclient.ResourceRouting{
		ResourceID:   resp.InstanceId,
		ResourceName: req.InstanceName,
		ResourceType: dbclient.ResourceTypeRedis,
		Vendor:       req.Vendor,
		OrgID:        req.OrgID,
		ClusterName:  req.ClusterName,
		ProjectID:    req.ProjectID,
		AddonID:      req.AddonID,
		Status:       dbclient.ResourceStatusAttached,
		RecordID:     r.ID,
	}
	return cbResp, routing, nil
}

func init() {
	factory := RedisFactory{BaseResourceFactory: &resource_factory.BaseResourceFactory{}}
	factory.Creator = creator
	factory.RecordType = dbclient.RecordTypeCreateAliCloudRedis
	err := resource_factory.Register(dbclient.ResourceTypeRedis, factory)
	if err != nil {
		panic(err)
	}
}
