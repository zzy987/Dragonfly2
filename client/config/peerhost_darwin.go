//go:build darwin
// +build darwin

/*
 *     Copyright 2020 The Dragonfly Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package config

import (
	"net"
	"time"

	"golang.org/x/time/rate"

	"d7y.io/dragonfly/v2/client/clientutil"
	"d7y.io/dragonfly/v2/manager/model"
	"d7y.io/dragonfly/v2/pkg/dfnet"
	"d7y.io/dragonfly/v2/pkg/net/fqdn"
	"d7y.io/dragonfly/v2/pkg/net/ip"
)

var peerHostConfig = DaemonOption{
	AliveTime:   clientutil.Duration{Duration: DefaultDaemonAliveTime},
	GCInterval:  clientutil.Duration{Duration: DefaultGCInterval},
	KeepStorage: false,
	Scheduler: SchedulerOption{
		Manager: ManagerOption{
			Enable:          false,
			RefreshInterval: 5 * time.Minute,
			SeedPeer: SeedPeerOption{
				Enable:    false,
				Type:      model.SeedPeerTypeSuperSeed,
				ClusterID: 1,
				KeepAlive: KeepAliveOption{
					Interval: 5 * time.Second,
				},
			},
		},
		NetAddrs: []dfnet.NetAddr{
			{
				Type: dfnet.TCP,
				Addr: "127.0.0.1:8002",
			},
		},
		ScheduleTimeout: clientutil.Duration{Duration: DefaultScheduleTimeout},
	},
	Host: HostOption{
		Hostname:       fqdn.FQDNHostname,
		ListenIP:       net.IPv4zero.String(),
		AdvertiseIP:    ip.IPv4,
		SecurityDomain: "",
		Location:       "",
		IDC:            "",
		NetTopology:    "",
	},
	Download: DownloadOption{
		DefaultPattern:       PatternP2P,
		CalculateDigest:      true,
		PieceDownloadTimeout: 30 * time.Second,
		GetPiecesMaxRetry:    100,
		TotalRateLimit: clientutil.RateLimit{
			Limit: rate.Limit(DefaultTotalDownloadLimit),
		},
		PerPeerRateLimit: clientutil.RateLimit{
			Limit: rate.Limit(DefaultPerPeerDownloadLimit),
		},
		DownloadGRPC: ListenOption{
			Security: SecurityOption{
				Insecure:  true,
				TLSVerify: true,
			},
			UnixListen: &UnixListenOption{
				Socket: "/tmp/dfdaemon.sock",
			},
		},
		PeerGRPC: ListenOption{
			Security: SecurityOption{
				Insecure:  true,
				TLSVerify: true,
			},
			TCPListen: &TCPListenOption{
				Listen: net.IPv4zero.String(),
				PortRange: TCPListenPortRange{
					Start: 65000,
					End:   65535,
				},
			},
		},
	},
	Upload: UploadOption{
		RateLimit: clientutil.RateLimit{
			Limit: rate.Limit(DefaultUploadLimit),
		},
		ListenOption: ListenOption{
			Security: SecurityOption{
				Insecure:  true,
				TLSVerify: false,
			},
			TCPListen: &TCPListenOption{
				Listen: net.IPv4zero.String(),
				PortRange: TCPListenPortRange{
					Start: 65002,
					End:   65535,
				},
			},
		},
	},
	ObjectStorage: ObjectStorageOption{
		Enable:      false,
		Filter:      "Expires&Signature&ns",
		MaxReplicas: 3,
		ListenOption: ListenOption{
			Security: SecurityOption{
				Insecure:  true,
				TLSVerify: true,
			},
			TCPListen: &TCPListenOption{
				Listen: net.IPv4zero.String(),
				PortRange: TCPListenPortRange{
					Start: 65004,
					End:   65535,
				},
			},
		},
	},
	Proxy: &ProxyOption{
		ListenOption: ListenOption{
			Security: SecurityOption{
				Insecure:  true,
				TLSVerify: false,
			},
			TCPListen: &TCPListenOption{
				Listen:    net.IPv4zero.String(),
				PortRange: TCPListenPortRange{},
			},
		},
	},
	Storage: StorageOption{
		TaskExpireTime: clientutil.Duration{
			Duration: DefaultTaskExpireTime,
		},
		StoreStrategy:          AdvanceLocalTaskStoreStrategy,
		Multiplex:              false,
		DiskGCThresholdPercent: 95,
	},
	Reload: ReloadOption{
		Interval: clientutil.Duration{
			Duration: time.Minute,
		},
	},
}
