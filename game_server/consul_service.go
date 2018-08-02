package main

import (
	"fmt"
	"net/http"
	"strconv"
)
import consulapi "github.com/hashicorp/consul/api"

func consulCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "consulCheck")
}

func registerServer() (bool, string) {
	//config := consulapi.DefaultConfig()
	//client, err := consulapi.NewClient(config)
	config := consulapi.Config{Address: cfg_server_addr_consul_agent}
	client, err := consulapi.NewClient(&config)
	if err != nil {
		return false, err.Error()
	}

	port, _ := strconv.Atoi(cfg_node_port)

	registration := new(consulapi.AgentServiceRegistration)
	registration.ID = cfg_node_id
	registration.Name = cfg_node_name
	registration.Port = port
	registration.Tags = cfg_node_tags
	registration.Address = cfg_node_addr
	registration.Check = &consulapi.AgentServiceCheck{
		HTTP:     fmt.Sprintf("http://%s:%s%s", registration.Address, cfg_node_check_port, cfg_node_check_url),
		Timeout:  cfg_node_check_timeout,
		Interval: cfg_node_check_interval,
		//DeregisterCriticalServiceAfter: cfg_node_check_deregister_timeout,
	}

	err = client.Agent().ServiceRegister(registration)

	if err != nil {
		return false, err.Error()
	}

	go startConsulCheckService()

	return true, ""
}

func startConsulCheckService() {
	http.HandleFunc("/check", consulCheck)
	http.ListenAndServe(fmt.Sprintf(":%s", cfg_node_check_port), nil)
}
