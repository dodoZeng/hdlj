package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
)
import (
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	pb "roisoft.com/hdlj/proto"
)

type http_service struct {
	wg   *sync.WaitGroup
	stop bool
}

func init() {
}

func NewHttpService() *http_service {
	return &http_service{
		wg:   &sync.WaitGroup{},
		stop: true,
	}
}

func (service *http_service) start_http_service(addr string) (bool, string) {

	if ok, err := conn_to_all_server(); !ok {
		return false, err
	}

	http.HandleFunc("/check", Check)
	http.HandleFunc("/account/signup", Signup)
	http.HandleFunc("/account/signin", Signin)
	http.HandleFunc("/account/serverlist", GetGameServerList)
	http.HandleFunc("/account/accountinfo", GetAccountData)
	http.HandleFunc("/account/checkid", CheckIDCard)
	go http.ListenAndServe(fmt.Sprintf("%s:%s", cfg_node_addr, cfg_node_port), nil)

	service.stop = false

	return true, ""
}

func (service *http_service) stop_http_service() {
	service.stop = true

	service.wg.Wait()
}

func Signup(w http.ResponseWriter, r *http.Request) {

	var in pb.SignupRequest

	if body, err := ioutil.ReadAll(r.Body); err != nil {
		glog.Errorf("Signup read body error. [err = %+v]\n", err)
	} else {
		if err := proto.Unmarshal(body, &in); err != nil {
			glog.Errorf("Signup unmarshal error. [err = %+v]\n", err)
		} else {
			out := signup(&in)
			buf, _ := proto.Marshal(out)

			// 返回数据
			w.Write(buf)
		}
	}
}

func Signin(w http.ResponseWriter, r *http.Request) {

	var in pb.SigninRequest

	if body, err := ioutil.ReadAll(r.Body); err != nil {
		glog.Errorf("Signin read body error. [err = %+v]\n", err)
	} else {
		if err := proto.Unmarshal(body, &in); err != nil {
			glog.Errorf("Signin unmarshal error. [err = %+v]\n", err)
		} else {
			out := signin(&in)
			buf, _ := proto.Marshal(out)

			// 返回数据
			w.Write(buf)
		}
	}
}

func GetGameServerList(w http.ResponseWriter, r *http.Request) {

	var in pb.SessionInfo

	if body, err := ioutil.ReadAll(r.Body); err != nil {
		glog.Errorf("GetGameServerList read body error. [err = %+v]\n", err)
	} else {
		if err := proto.Unmarshal(body, &in); err != nil {
			glog.Errorf("GetGameServerList unmarshal error. [err = %+v]\n", err)
		} else {
			out := getGameServerList(&in)
			buf, _ := proto.Marshal(out)

			// 返回数据
			w.Write(buf)
		}
	}
}

func GetAccountData(w http.ResponseWriter, r *http.Request) {

	var in pb.SessionInfo

	if body, err := ioutil.ReadAll(r.Body); err != nil {
		glog.Errorf("GetAccountData read body error. [err = %+v]\n", err)
	} else {
		if err := proto.Unmarshal(body, &in); err != nil {
			glog.Errorf("GetAccountData unmarshal error. [err = %+v]\n", err)
		} else {
			out := getAccountData(&in)
			buf, _ := proto.Marshal(out)

			// 返回数据
			w.Write(buf)
		}
	}
}

func CheckIDCard(w http.ResponseWriter, r *http.Request) {
	var in pb.CheckIdCardRequest

	if body, err := ioutil.ReadAll(r.Body); err != nil {
		glog.Errorf("CheckIDCard read body error. [err = %+v]\n", err)
	} else {
		if err := proto.Unmarshal(body, &in); err != nil {
			glog.Errorf("CheckIDCard unmarshal error. [err = %+v]\n", err)
		} else {
			out := checkIDCard(&in)
			buf, _ := proto.Marshal(out)

			// 返回数据
			w.Write(buf)
		}
	}
}

func Check(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "consulCheck")
}

func getPostParam(param string, r *http.Request) string {

	if len(param) > 0 {
		if r.MultipartForm != nil {
			values := r.MultipartForm.Value[param]
			if len(values) > 0 {
				return values[0]
			}
		} else {
			return r.PostFormValue(param)
		}
	}

	return ""
}
