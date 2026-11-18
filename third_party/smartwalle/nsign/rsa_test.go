package nsign_test

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"github.com/smartwalle/nsign"
	"net/url"
	"testing"
)

var pri *rsa.PrivateKey
var pub *rsa.PublicKey

func ParsePKCS1PrivateKey(data []byte) (key *rsa.PrivateKey, err error) {
	var block *pem.Block
	block, _ = pem.Decode(data)
	if block == nil {
		return nil, errors.New("private key error")
	}

	key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return key, err
}

func ParsePKCS1PublicKey(data []byte) (key *rsa.PublicKey, err error) {
	var block *pem.Block
	block, _ = pem.Decode(data)
	if block == nil {
		return nil, errors.New("public key error")
	}

	var pubInterface interface{}
	pubInterface, err = x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key error")
	}

	return key, err
}

func init() {
	var privStr = `
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAttTw+Qwdmj8GfFvdMvoODllVIIertNXt/O6f5jUbIMlzXlHQ
W6Yb60DmTMfFWqVXLBuhCfL+xiNb6JhMWFaFV+Rxh1OSFxiSB917VSxt7zrc/oU8
UtKK/YtUXBES3XeeSaDfJPSx8u/oM2Q0L0b7kHTQdXixmzzdc3m23DnVv18Vaxol
Ql2JHjdKq89FfUdqJRPLDROkoEUFY1m/D6IfW/E+w3hEinmOL7JvieLW8cNmtnqB
2+KxS0F0ZCX1Ur36Uz5DNLTEXkeAB49RKWli7qbXICdmC8V9wQS2LlNXLDHAK9Vv
YPkb5fpYpwJHOcW/eaUvXhlRcOBBKKBc6ChsWwIDAQABAoIBAHIkgQjKwpRwsojj
BTb1G99jcBzt9ongYULeax9amkQe4JLK+wysqJN3og/fTFuqC8Eywpgmh16F5rRQ
mKEx6u+TZDk7OGKI6WpVoNDs+vk2w492+NEwNqvR12nfEusG6eBHuegliA8GLe4f
qeC2LQnjk90y65biKdMU8s+Mn+Bn2w+81aF9MC9H7437NGcXt2T4HXqBltzPkyPv
3rbq6ukhwm43MdD+mnUUIbKEfCIY4MLxqVyegOoaqgZmeDmer7zCE0VoOlYx7LKj
fGRY19ZiebBBHMxshU6cSpJOJUDVbtM6jIj6xIByW5yiWJ576FUYR5uzZw7kja4q
EKjL32ECgYEA3WSikB0UbLpUM7PUAieCPaVm7Z5dOag+VDHYyHQu8IVnfiId24lW
o7WJPTKgP8TB48HO9XPeLjVX9pJ4kga0KaMV8712+xgEGfTvzBboBMII6VrJLHoe
Nm/VW/Z2jnqT2NEHRGi/6ootSDW3Y+n6Z1HYHSWBhx4NgSFBIG2Lq1ECgYEA02k4
UsOywQY9W9Hg7oPWvXW/qGbNr1KcRPCjgOEUJ5YE7yziQMJQk2VgzyPr3CRFb+LK
pKmXK3qzT/l6/bjtYbg8e34NTaYET7NuDYK+9ZoKmp5p/Mrm3m6tFuP417ERc5kw
txBhGjaMaV2Dyuk0DEwP4+OX3e5D47t3GEVzWesCgYEAqQ/2S9LJDBZlwm2qklKV
VHoAVag/TI47upOuFbUTOzzEQT/QSRthe/Ze7MrCMLAR19jiL/HhUqwNlg9X3zsb
TUhqtQyT1T8Lsr6md9VuLGP35isbwMkoVS2lYmqdMkRMrp9Ay26qT3JhDelnN/cu
7sNDI07G0OWULm20jAzbQWECgYAvAo24sbWVAHsyaLgYsY5VsNI+cyW3n6oKFmx1
IxelOdG9EYD6H3tlWzyssvMmj5Y1K/wdo2xvCajH1tUHiFCY0yv31e7FXm5E/sQx
Euq9mFRVT3aH9OJjgb7RgT37UG9uAhl4C5dcCdctMtM0kqi1N1CtPxWtDudfw3bX
GdJtowKBgQDRwDe7CDlG3C3Sa/Tbvxq5RrRcNQC4vXHOsXVkGE1Now4oqFxP/ejp
Ff0vOi7A/qDaFKR4wNY7rAO+I5ff4nwEufjXh28mkJKhMcJF0l/HIsbVQgAfxciB
2cEf4Lm82omNYrbX/RsWz6Glxpz4DPejgTqyMH8sVGz+HmwqCcIHhA==
-----END RSA PRIVATE KEY-----
`

	var pubStr = `
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAttTw+Qwdmj8GfFvdMvoO
DllVIIertNXt/O6f5jUbIMlzXlHQW6Yb60DmTMfFWqVXLBuhCfL+xiNb6JhMWFaF
V+Rxh1OSFxiSB917VSxt7zrc/oU8UtKK/YtUXBES3XeeSaDfJPSx8u/oM2Q0L0b7
kHTQdXixmzzdc3m23DnVv18VaxolQl2JHjdKq89FfUdqJRPLDROkoEUFY1m/D6If
W/E+w3hEinmOL7JvieLW8cNmtnqB2+KxS0F0ZCX1Ur36Uz5DNLTEXkeAB49RKWli
7qbXICdmC8V9wQS2LlNXLDHAK9VvYPkb5fpYpwJHOcW/eaUvXhlRcOBBKKBc6Chs
WwIDAQAB
-----END PUBLIC KEY-----
`

	pri, _ = ParsePKCS1PrivateKey([]byte(privStr))
	pub, _ = ParsePKCS1PublicKey([]byte(pubStr))
}

func TestRSA_SignBytes(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewRSAMethod(crypto.SHA1, pri, pub)))

	var src = "jsapi_ticket=sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg&noncestr=Wm3WZYTPz0wzccnW&timestamp=1414587457&url=http://mp.weixin.qq.com?params=value"
	var rb, err = signer.SignBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}

	var r = hex.EncodeToString(rb)
	if r != "727e2ae360c2db669703b473cbfb2c879e510f97c346b67a89b9c721b91506e4269383ad632fec8c1a0b2163312fb1d795ab7b2360108ec95e6976c073215e8f033791ccd35600206afd0e2728a29711c63ee6ef755d6b13c83d703c52b3a0f1f302f285318f2a3f82aa57fa031e665a2ee601d59ed63295c40472bd97f3216148d0f043e0c6e8bca079b34df47ac234dbdd9d99f5fcbda5e0723f82104c058d7a60e16c0e2d974908bb3c2bb96d6b6d67cc9470692ef74c80eb5292359b5e64183dace71a34a3aa532b010f9df44f9cc90e7b917aa4fac962964b744864a4c9ba3730c0f897dad71c5a7343a2c60a76e29cbcece4748597bdeab900197f5411" {
		t.Fatal("sha1 签名错误")
	}
}

func TestRSA_VerifyBytes(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewRSAMethod(crypto.SHA1, pri, pub)))
	var src = "jsapi_ticket=sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg&noncestr=Wm3WZYTPz0wzccnW&timestamp=1414587457&url=http://mp.weixin.qq.com?params=value"

	var sb, _ = hex.DecodeString("727e2ae360c2db669703b473cbfb2c879e510f97c346b67a89b9c721b91506e4269383ad632fec8c1a0b2163312fb1d795ab7b2360108ec95e6976c073215e8f033791ccd35600206afd0e2728a29711c63ee6ef755d6b13c83d703c52b3a0f1f302f285318f2a3f82aa57fa031e665a2ee601d59ed63295c40472bd97f3216148d0f043e0c6e8bca079b34df47ac234dbdd9d99f5fcbda5e0723f82104c058d7a60e16c0e2d974908bb3c2bb96d6b6d67cc9470692ef74c80eb5292359b5e64183dace71a34a3aa532b010f9df44f9cc90e7b917aa4fac962964b744864a4c9ba3730c0f897dad71c5a7343a2c60a76e29cbcece4748597bdeab900197f5411")

	if err := signer.VerifyBytes([]byte(src), sb); err != nil {
		t.Fatal("sha1 验签错误:", err)
	}
}

func TestRSA_SignValues(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewRSAMethod(crypto.SHA1, pri, pub)))
	var values = url.Values{}
	values.Add("jsapi_ticket", "sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg")
	values.Add("noncestr", "Wm3WZYTPz0wzccnW")
	values.Add("timestamp", "1414587457")
	values.Add("url", "http://mp.weixin.qq.com?params=value")

	var rb, err = signer.SignValues(values)
	if err != nil {
		t.Fatal(err)
	}

	var sb, _ = hex.DecodeString("727e2ae360c2db669703b473cbfb2c879e510f97c346b67a89b9c721b91506e4269383ad632fec8c1a0b2163312fb1d795ab7b2360108ec95e6976c073215e8f033791ccd35600206afd0e2728a29711c63ee6ef755d6b13c83d703c52b3a0f1f302f285318f2a3f82aa57fa031e665a2ee601d59ed63295c40472bd97f3216148d0f043e0c6e8bca079b34df47ac234dbdd9d99f5fcbda5e0723f82104c058d7a60e16c0e2d974908bb3c2bb96d6b6d67cc9470692ef74c80eb5292359b5e64183dace71a34a3aa532b010f9df44f9cc90e7b917aa4fac962964b744864a4c9ba3730c0f897dad71c5a7343a2c60a76e29cbcece4748597bdeab900197f5411")
	if bytes.Compare(rb, sb) != 0 {
		t.Fatal("sha1 签名错误")
	}
}

func TestRSA_VerifyValues(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewRSAMethod(crypto.SHA1, pri, pub)))
	var values = url.Values{}
	values.Add("jsapi_ticket", "sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg")
	values.Add("noncestr", "Wm3WZYTPz0wzccnW")
	values.Add("timestamp", "1414587457")
	values.Add("url", "http://mp.weixin.qq.com?params=value")

	var sb, _ = hex.DecodeString("727e2ae360c2db669703b473cbfb2c879e510f97c346b67a89b9c721b91506e4269383ad632fec8c1a0b2163312fb1d795ab7b2360108ec95e6976c073215e8f033791ccd35600206afd0e2728a29711c63ee6ef755d6b13c83d703c52b3a0f1f302f285318f2a3f82aa57fa031e665a2ee601d59ed63295c40472bd97f3216148d0f043e0c6e8bca079b34df47ac234dbdd9d99f5fcbda5e0723f82104c058d7a60e16c0e2d974908bb3c2bb96d6b6d67cc9470692ef74c80eb5292359b5e64183dace71a34a3aa532b010f9df44f9cc90e7b917aa4fac962964b744864a4c9ba3730c0f897dad71c5a7343a2c60a76e29cbcece4748597bdeab900197f5411")

	if err := signer.VerifyValues(values, sb); err != nil {
		t.Fatal("sha1 验签错误:", err)
	}
}
