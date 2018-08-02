package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"roisoft.com/hdlj/common"
	"roisoft.com/hdlj/common/config"
)

const (
	agent_tag = "hdlj.agent.cluster"
	game_tag  = "hdlj.game.cluster"
)

var (
	cfg_file = "./master_server.cfg"

	cfg_node_id                       = "hdlj.master.test"
	cfg_node_name                     = "hdlj.master.test"
	cfg_node_tags                     = []string{"hdlj.master.cluster"}
	cfg_node_check_port               = "8000"
	cfg_node_check_url                = "/check"
	cfg_node_check_timeout            = "5s"
	cfg_node_check_interval           = "10s"
	cfg_node_check_deregister_timeout = "30s"
	cfg_node_addr                     = "0.0.0.0"
	cfg_node_port                     = "50000"

	cfg_server_addr_consul_agent = "0.0.0.0:8500"
	cfg_server_addr_redis        = "0.0.0.0:6379"
	cfg_server_pwd_redis         = ""

	cfg_log_custom_level = "3"
	cfg_log_dir          = "./log"

	cfg_redis_client_size_per_cpu = 10
	cfg_client_pool_size          = 0
	cfg_agent_cluster_node_total  = 0

	cfg_get_config_interval = 5 * int64(1e9)

	cfg_session_expiration_in_hour = 72

	cfg_node_id_suffix = ""

	cfg_player_id_config = "./roisoft.playerid.ini" // 玩家ID开始编号
)

var gMapConfig map[string]string

func init() {
	gMapConfig = make(map[string]string)
}

func get_config_from_consul() (bool, string) {
	client, err := consulapi.NewClient(consulapi.DefaultConfig())
	if err != nil {
		return false, err.Error()
	}

	if services, err := client.Agent().Services(); err != nil {
		return false, err.Error()
	} else {
		if checks, err := client.Agent().Checks(); err != nil {
			return false, err.Error()
		} else {
			for _, v := range services {
				if common.CheckString(agent_tag, v.Tags) {
					if agentCluster.node_total >= agent_cluster_node_max {
						return false, "overflow the max number of agent server"
					}

					service_key := fmt.Sprintf("service:%s", v.ID)
					if index, ok := agentCluster.id2index.Load(v.ID); !ok {
						agentCluster.id2index.Store(v.ID, agentCluster.node_total)
						agentCluster.nodes[agentCluster.node_total].Id = v.ID
						agentCluster.nodes[agentCluster.node_total].Addr = v.Address
						agentCluster.nodes[agentCluster.node_total].Port = v.Port
						if c, ok := checks[service_key]; ok && c.Status == "passing" {
							agentCluster.nodes[agentCluster.node_total].Available = true
						}
						agentCluster.node_total++
					} else {
						i := index.(uint32)
						if c, ok := checks[service_key]; ok {
							if c.Status == "passing" {
								agentCluster.nodes[i].Available = true
							} else {
								agentCluster.nodes[i].Available = false
							}
						}
					}
				} else if common.CheckString(game_tag, v.Tags) {
					if gameCluster.node_total >= game_cluster_node_max {
						return false, "overflow the max number of game server"
					}

					service_key := fmt.Sprintf("service:%s", v.ID)
					if index, ok := gameCluster.id2index.Load(v.ID); !ok {
						gameCluster.id2index.Store(v.ID, gameCluster.node_total)
						gameCluster.nodes[gameCluster.node_total].Id = v.ID
						gameCluster.nodes[gameCluster.node_total].Addr = fmt.Sprintf("%s:%d", v.Address, v.Port)
						gameCluster.nodes[gameCluster.node_total].Port = v.Port
						gameCluster.nodes[gameCluster.node_total].Name = v.Service
						if c, ok := checks[service_key]; ok && c.Status == "passing" {
							gameCluster.nodes[gameCluster.node_total].Available = true
						}
						gameCluster.node_total++
					} else {
						i := index.(uint32)
						if c, ok := checks[service_key]; ok {
							if c.Status == "passing" {
								gameCluster.nodes[i].Available = true
							} else {
								gameCluster.nodes[i].Available = false
							}
						}
					}
				}
			}
		}
	}
	return true, ""
}

func LoadConfig(f string) (bool, string) {

	cfg, err := config.ReadDefault(f)
	if err != nil {
		return false, "Fail to read config file. " + err.Error()
	}
	for _, sec := range cfg.Sections() {
		opts, _ := cfg.SectionOptions(sec)
		for _, opt := range opts {
			gMapConfig[string(sec+"."+opt)], _ = cfg.String(sec, opt)
		}
	}

	// 把配置读到变量里
	if str, ok := gMapConfig["server.node_id"]; ok {
		cfg_node_id = str
	}
	if str, ok := gMapConfig["server.node_name"]; ok {
		cfg_node_name = str
	}
	if str, ok := gMapConfig["server.node_tags"]; ok {
		cfg_node_tags = strings.Split(str, ",")
	}
	if str, ok := gMapConfig["server.node_check_port"]; ok {
		cfg_node_check_port = str
	}
	if str, ok := gMapConfig["server.node_check_url"]; ok {
		cfg_node_check_url = str
	}
	if str, ok := gMapConfig["server.node_check_timeout"]; ok {
		cfg_node_check_timeout = str
	}
	if str, ok := gMapConfig["server.node_check_interval"]; ok {
		cfg_node_check_interval = str
	}
	if str, ok := gMapConfig["server.node_check_deregister_timeout"]; ok {
		cfg_node_check_deregister_timeout = str
	}
	if str, ok := gMapConfig["server.node_addr"]; ok {
		cfg_node_addr = str
	}
	if str, ok := gMapConfig["server.node_port"]; ok {
		cfg_node_port = str
	}
	if str, ok := gMapConfig["server.log_dir"]; ok {
		cfg_log_dir = str
	}
	if str, ok := gMapConfig["server.log_custom_level"]; ok {
		cfg_log_custom_level = str
	}
	if str, ok := gMapConfig["server.server_addr_consul_agent"]; ok {
		cfg_server_addr_consul_agent = str
	}
	if str, ok := gMapConfig["server.server_addr_redis"]; ok {
		cfg_server_addr_redis = str
	}
	if str, ok := gMapConfig["server.server_pwd_redis"]; ok {
		cfg_server_pwd_redis = str
	}
	if str, ok := gMapConfig["server.redis_client_pool_size"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_redis_client_size_per_cpu = (int)(v) * runtime.NumCPU()
	}
	if str, ok := gMapConfig["server.get_config_interval"]; ok {
		v, err := time.ParseDuration(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_get_config_interval = (int64)(v)
	}

	// game
	if str, ok := gMapConfig["game.session_expiration_in_hour"]; ok {
		v, err := strconv.Atoi(str)
		if nil != err {
			return false, err.Error()
		}
		cfg_session_expiration_in_hour = v
	}

	return true, ""
}
