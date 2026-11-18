package nsign_test

import (
	"bytes"
	"crypto"
	_ "crypto/md5"
	_ "crypto/sha1"
	"encoding/hex"
	"github.com/smartwalle/nsign"
	"net/url"
	"testing"
)

func BenchmarkHash_SignValues(b *testing.B) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewHashMethod(crypto.MD5)))
	for i := 0; i < b.N; i++ {
		var form = make(url.Values, 0)
		form.Set("c", "30")
		form.Set("b", "20")
		form.Set("a", "30")
		_, _ = signer.SignValues(form)
	}
}

func TestHash_SignBytes(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewHashMethod(crypto.SHA1)))
	var src = "jsapi_ticket=sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg&noncestr=Wm3WZYTPz0wzccnW&timestamp=1414587457&url=http://mp.weixin.qq.com?params=value"
	var rb, err = signer.SignBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var r = hex.EncodeToString(rb)
	if r != "0f9de62fce790f9a083d5c99e95740ceb90c27ed" {
		t.Fatal("sha1 签名错误")
	}
}

func TestHash_VerifyBytes(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewHashMethod(crypto.SHA1)))
	var src = "jsapi_ticket=sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg&noncestr=Wm3WZYTPz0wzccnW&timestamp=1414587457&url=http://mp.weixin.qq.com?params=value"

	var sb, _ = hex.DecodeString("0f9de62fce790f9a083d5c99e95740ceb90c27ed")

	if err := signer.VerifyBytes([]byte(src), sb); err != nil {
		t.Fatal("sha1 验签错误:", err)
	}
}

func TestHash_SignValues(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewHashMethod(crypto.SHA1)))
	var values = url.Values{}
	values.Add("jsapi_ticket", "sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg")
	values.Add("noncestr", "Wm3WZYTPz0wzccnW")
	values.Add("timestamp", "1414587457")
	values.Add("url", "http://mp.weixin.qq.com?params=value")

	var rb, err = signer.SignValues(values)
	if err != nil {
		t.Fatal(err)
	}

	var sb, _ = hex.DecodeString("0f9de62fce790f9a083d5c99e95740ceb90c27ed")
	if bytes.Compare(rb, sb) != 0 {
		t.Fatal("sha1 签名错误")
	}
}

func TestHash_VerifyValues(t *testing.T) {
	var signer = nsign.New(nsign.WithMethod(nsign.NewHashMethod(crypto.SHA1)))
	var values = url.Values{}
	values.Add("jsapi_ticket", "sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg")
	values.Add("noncestr", "Wm3WZYTPz0wzccnW")
	values.Add("timestamp", "1414587457")
	values.Add("url", "http://mp.weixin.qq.com?params=value")

	var sb, _ = hex.DecodeString("0f9de62fce790f9a083d5c99e95740ceb90c27ed")

	if err := signer.VerifyValues(values, sb); err != nil {
		t.Fatal("sha1 验签错误:", err)
	}
}
