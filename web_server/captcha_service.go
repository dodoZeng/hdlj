package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang/glog"
	"github.com/mojocn/base64Captcha"
	"roisoft.com/hdlj/common"
	//"golang.org/x/net/websocket"
)

var (
	// 数字
	cfgCaptchaDigit = base64Captcha.ConfigDigit{
		Height:     80,
		Width:      240,
		MaxSkew:    0.7,
		DotCount:   80,
		CaptchaLen: 4,
	}
	// 声音验证码配置
	cfgCaptchaAudio = base64Captcha.ConfigAudio{
		CaptchaLen: 4,
		Language:   "zh",
	}
	// 字符,公式,验证码配置
	cfgCaptchaCharacter = base64Captcha.ConfigCharacter{
		Height: 80,
		Width:  240,
		// CaptchaModeNumber:数字,CaptchaModeAlphabet:字母,CaptchaModeArithmetic:算术,CaptchaModeNumberAlphabet:数字字母混合.
		Mode:               base64Captcha.CaptchaModeArithmetic,
		ComplexOfNoiseText: base64Captcha.CaptchaComplexLower,
		ComplexOfNoiseDot:  base64Captcha.CaptchaComplexLower,
		IsShowHollowLine:   false,
		IsShowNoiseDot:     true,
		IsShowNoiseText:    false,
		IsShowSlimeLine:    false,
		IsShowSineLine:     false,
		CaptchaLen:         4,
	}

	redisCluster common.RedisCluster
)

//ConfigJsonBody json request body.
type ConfigJsonBody struct {
	Id              string
	CaptchaType     string
	VerifyValue     string
	ConfigAudio     base64Captcha.ConfigAudio
	ConfigCharacter base64Captcha.ConfigCharacter
	ConfigDigit     base64Captcha.ConfigDigit
}

func testCaptcha() {
	//create a characters captcha.
	//key, _ := base64Captcha.GenerateCaptcha("", cfgCaptchaCharacter)
	//fmt.Println(key)

	var postParameters ConfigJsonBody

	str := `{"CaptchaType": "character", "Id":"sUG0sSGf2AFsEGRBuJ6o", "VerifyValue": "96"}`
	if err := json.Unmarshal([]byte(str), &postParameters); err != nil {
		fmt.Println(err)
	}
	fmt.Println(postParameters)
}

func generateCaptchaHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	//create a audio captcha.
	//idKeyA, capA := base64Captcha.GenerateCaptcha("", cfgCaptchaAudio)
	//以 base64 编码
	//base64stringA := base64Captcha.CaptchaWriteToBase64Encoding(capA)

	//create a characters captcha.
	idKeyC, capC := base64Captcha.GenerateCaptcha("", cfgCaptchaCharacter)
	//以 base64 编码
	base64stringC := base64Captcha.CaptchaWriteToBase64Encoding(capC)

	//create a digits captcha.
	//idKeyD, capD := base64Captcha.GenerateCaptcha("", cfgCaptchaDigit)
	//以 base64 编码
	//base64stringD := base64Captcha.CaptchaWriteToBase64Encoding(capD)

	//fmt.Println(idKeyA, base64stringA, "\n")
	//fmt.Println(idKeyC, base64stringC, "\n")
	//fmt.Println(idKeyD, base64stringD, "\n")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	body := map[string]interface{}{"code": 1, "data": base64stringC, "id": idKeyC, "msg": "success"}
	json.NewEncoder(w).Encode(body)
}

// base64Captcha create http handler
func generateCaptchaHandler2(w http.ResponseWriter, r *http.Request) {
	//parse request parameters
	decoder := json.NewDecoder(r.Body)
	var postParameters ConfigJsonBody
	err := decoder.Decode(&postParameters)
	if err != nil {
		glog.Infoln(err)
	}
	fmt.Println(postParameters)
	defer r.Body.Close()

	//create base64 encoding captcha
	var config interface{}
	switch postParameters.CaptchaType {
	case "audio":
		config = postParameters.ConfigAudio
	case "character":
		config = postParameters.ConfigCharacter
	default:
		config = postParameters.ConfigDigit
	}
	captchaId, captcaInterfaceInstance := base64Captcha.GenerateCaptcha(postParameters.Id, config)
	base64blob := base64Captcha.CaptchaWriteToBase64Encoding(captcaInterfaceInstance)

	//or you can just write the captcha content to the httpResponseWriter.
	//before you put the captchaId into the response COOKIE.
	//captcaInterfaceInstance.WriteTo(w)

	//set json response
	//设置json响应
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	body := map[string]interface{}{"code": 1, "data": base64blob, "captchaId": captchaId, "msg": "success"}
	json.NewEncoder(w).Encode(body)
}

// base64Captcha verify http handler
func captchaVerifyHandle(w http.ResponseWriter, r *http.Request) {

	//parse request parameters
	decoder := json.NewDecoder(r.Body)

	var postParameters ConfigJsonBody
	err := decoder.Decode(&postParameters)
	if err != nil {
		glog.Infoln(err)
	}
	defer r.Body.Close()
	//verify the captcha
	verifyResult := base64Captcha.VerifyCaptcha(postParameters.Id, postParameters.VerifyValue)
	//fmt.Println("postParameters:", postParameters)

	//set json response
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	body := map[string]interface{}{"code": "error", "data": "", "msg": "captcha failed"}
	if verifyResult {
		token := common.MakeToken()
		redis := redisCluster.GetNodeByString(token)

		if redis != nil {
			// save token to redis
			//fmt.Println("token = ", token)
			redis.Set(fmt.Sprintf(common.Redis_Key_Captcha_Format, token), "", time.Duration(cfg_captcha_expiration))

			// send token to client
			body = map[string]interface{}{"code": "success", "data": token, "msg": "captcha verified"}
		} else {
			body = map[string]interface{}{"code": "error", "data": "", "msg": "no redis client"}
		}
	}
	json.NewEncoder(w).Encode(body)
}

func consulCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "consulCheck")
}

func startCaptchaService() (bool, string) {

	base64Captcha.Expiration = time.Duration(cfg_captcha_expiration)

	http.Handle("/", http.FileServer(http.Dir("./static")))
	//api for create captcha
	http.HandleFunc("/captcha/get", generateCaptchaHandler)
	//api for verify captcha
	http.HandleFunc("/captcha/chk", captchaVerifyHandle)
	// for consul check
	http.HandleFunc("/check", consulCheck)

	go http.ListenAndServe(fmt.Sprintf("%s:%s", cfg_node_addr, cfg_node_port), nil)

	return true, ""
}
