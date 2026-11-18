package ncrypto_test

import (
	"github.com/smartwalle/ncrypto"
	"testing"
)

func TestDecodePrivateKey_RSA_PKCS1(t *testing.T) {
	var testTbl = []struct{ key string }{
		{
			key: `-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQCzXV/spaX9+eOjM5f12W6eDTtszU9f9rgpXG4EQwzZI3WM5+Fe
+9Bn6NQQILfF1o3Z+3BEzHMMcYwxrQw/toq2o6JPchbUK7eArKc6pl/GV3uIefZd
Kncz5bZvCFMgiJrpy75lYKhJgotQFEfQd+ks2t0gtC007uOjmY9QDB2EVQIDAQAB
AoGAMruhi0UbW2gYHCxWuiJDKI9jlJXJ8sHNO126fJgehTiDYlSgKYaeXxW7DcjD
UkEqpFJ7YepWTFm9prtksIzIVQFNNjstI6cvowVF2t+lWf7mIB4w0ugarVd+SXss
QK830Og3kjtZ84a3BbC6uf3a/qcgoIO8Sj1VnzOJ8fEYl+0CQQDeG6JhauGDOC8o
CTwbFs9QPpjwGnp7UkYAJNg7jn4uBSVeg4lwb5uj9TshLSp49geNkPcWeCythuiz
1jvoTqEjAkEAzrwIBxUPT1WmcDUXAkVPaQNADDbhMZLdw5nHZEUVwmO3o1FkJky4
MLjLjT977400mhsnsQCy4sAWUZs6aEyoJwJARK3U2zy6eOHhqwaYAGRgPJbuoaf+
Ya3CGX9LIbdhCwfqUzxnPk40mVFWNF8L+BVTppHB5b/JSOsjf6BqK95McwJBAL+k
vUhbdHrV6lmgTXkUaV3u3mO0SCPdgui9WIKSLG6sY+LpI48BlcnMtR12WVyjKL0n
KS9Dd5EOAmKaJJXlYgcCQGWbWCn9KUDUqpm4o3wr5nwXzlS74XYZo65UAM7TSzHR
pcovfv5uiQ0VRLImWeiSXKK2aTOBGn5eKbevRTxN07k=
-----END RSA PRIVATE KEY-----`,
		},
		{
			key: `MIICXAIBAAKBgQCzXV/spaX9+eOjM5f12W6eDTtszU9f9rgpXG4EQwzZI3WM5+Fe
+9Bn6NQQILfF1o3Z+3BEzHMMcYwxrQw/toq2o6JPchbUK7eArKc6pl/GV3uIefZd
Kncz5bZvCFMgiJrpy75lYKhJgotQFEfQd+ks2t0gtC007uOjmY9QDB2EVQIDAQAB
AoGAMruhi0UbW2gYHCxWuiJDKI9jlJXJ8sHNO126fJgehTiDYlSgKYaeXxW7DcjD
UkEqpFJ7YepWTFm9prtksIzIVQFNNjstI6cvowVF2t+lWf7mIB4w0ugarVd+SXss
QK830Og3kjtZ84a3BbC6uf3a/qcgoIO8Sj1VnzOJ8fEYl+0CQQDeG6JhauGDOC8o
CTwbFs9QPpjwGnp7UkYAJNg7jn4uBSVeg4lwb5uj9TshLSp49geNkPcWeCythuiz
1jvoTqEjAkEAzrwIBxUPT1WmcDUXAkVPaQNADDbhMZLdw5nHZEUVwmO3o1FkJky4
MLjLjT977400mhsnsQCy4sAWUZs6aEyoJwJARK3U2zy6eOHhqwaYAGRgPJbuoaf+
Ya3CGX9LIbdhCwfqUzxnPk40mVFWNF8L+BVTppHB5b/JSOsjf6BqK95McwJBAL+k
vUhbdHrV6lmgTXkUaV3u3mO0SCPdgui9WIKSLG6sY+LpI48BlcnMtR12WVyjKL0n
KS9Dd5EOAmKaJJXlYgcCQGWbWCn9KUDUqpm4o3wr5nwXzlS74XYZo65UAM7TSzHR
pcovfv5uiQ0VRLImWeiSXKK2aTOBGn5eKbevRTxN07k=`,
		},
		{
			key: `MIICXAIBAAKBgQCzXV/spaX9+eOjM5f12W6eDTtszU9f9rgpXG4EQwzZI3WM5+Fe+9Bn6NQQILfF1o3Z+3BEzHMMcYwxrQw/toq2o6JPchbUK7eArKc6pl/GV3uIefZdKncz5bZvCFMgiJrpy75lYKhJgotQFEfQd+ks2t0gtC007uOjmY9QDB2EVQIDAQABAoGAMruhi0UbW2gYHCxWuiJDKI9jlJXJ8sHNO126fJgehTiDYlSgKYaeXxW7DcjDUkEqpFJ7YepWTFm9prtksIzIVQFNNjstI6cvowVF2t+lWf7mIB4w0ugarVd+SXssQK830Og3kjtZ84a3BbC6uf3a/qcgoIO8Sj1VnzOJ8fEYl+0CQQDeG6JhauGDOC8oCTwbFs9QPpjwGnp7UkYAJNg7jn4uBSVeg4lwb5uj9TshLSp49geNkPcWeCythuiz1jvoTqEjAkEAzrwIBxUPT1WmcDUXAkVPaQNADDbhMZLdw5nHZEUVwmO3o1FkJky4MLjLjT977400mhsnsQCy4sAWUZs6aEyoJwJARK3U2zy6eOHhqwaYAGRgPJbuoaf+Ya3CGX9LIbdhCwfqUzxnPk40mVFWNF8L+BVTppHB5b/JSOsjf6BqK95McwJBAL+kvUhbdHrV6lmgTXkUaV3u3mO0SCPdgui9WIKSLG6sY+LpI48BlcnMtR12WVyjKL0nKS9Dd5EOAmKaJJXlYgcCQGWbWCn9KUDUqpm4o3wr5nwXzlS74XYZo65UAM7TSzHRpcovfv5uiQ0VRLImWeiSXKK2aTOBGn5eKbevRTxN07k=`,
		},
	}

	for _, test := range testTbl {
		var _, err = ncrypto.DecodePrivateKey([]byte(test.key)).PKCS1().RSAPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecodePrivateKey_RSA_PKCS8(t *testing.T) {
	var testTbl = []struct{ key string }{
		{
			key: `-----BEGIN PRIVATE KEY-----
MIICdQIBADANBgkqhkiG9w0BAQEFAASCAl8wggJbAgEAAoGBAMiVMWPJXP2B9fWK
v18CENwPZbJfOasLI9MHurbzs6rCvlXTmiG+hsWcTw8KkoefFK3MfqUBIxnOM5yb
ai6l7f1ODfF01qzk0FudSbkSQsA87tXZdLbwxEEbbWBNbs4BPBirLvxa7AsVAto0
iHEcq2IKLt87h4MYGfP4vZwuSBXNAgMBAAECgYBxVYswnMhEHTiCYsE6x4oLLVAC
9zc4Y/T7+jQPx6dO5vZwvD0sr+Cqq2UoVIrywnoGsbMlPH0+yXn0FQRsEylio6a9
vKdSybLa6fW26sWEua+ZlIHemGFvHQ9XNrlJbSKgM4T9HvC5bs9L6KXsSLNQUcqI
P1Y91PkG+2IkihKiwQJBAPgDqndHjDjdkHDEl59dBnMEF8hUEO0ziu3OjwnPlzv1
j6wNPDmdXq9U7J20FtujdYHBEh2sP5f9nuLH4tRiJqUCQQDPCo7djpQTUZk9hb8t
+4GTPoXnA/NbvMte+nHRiPO47ZC9D1tOjJKZlFuRCY0Meo2wPmO0DUCva2EnUX3s
PrIJAkA0y5r7H0jzRf8cck0QiJ351/I0G+kqhWFatDDw1rcL9X8rEfozDZP9YOep
vo9rHAXEpFP16xfyg/PRtNlNesNdAkAjGr8ugcZJoERDUjIgMcy+kpNRoDHbFB/H
ct9pj7cDXAR2iewJXXxd3fHInb30p7LudyWgmb6l/6bxa7fWHqtBAkAbJuWa15Rn
NgXtScAjuPVNbTIztwpCG+sBp3zZPVitEqrmiUfpcAP7XuuMHhTHn2WVJ0+icZY1
jN0pFHTUdTCS
-----END PRIVATE KEY-----`,
		},
		{
			key: `MIICdQIBADANBgkqhkiG9w0BAQEFAASCAl8wggJbAgEAAoGBAMiVMWPJXP2B9fWK
v18CENwPZbJfOasLI9MHurbzs6rCvlXTmiG+hsWcTw8KkoefFK3MfqUBIxnOM5yb
ai6l7f1ODfF01qzk0FudSbkSQsA87tXZdLbwxEEbbWBNbs4BPBirLvxa7AsVAto0
iHEcq2IKLt87h4MYGfP4vZwuSBXNAgMBAAECgYBxVYswnMhEHTiCYsE6x4oLLVAC
9zc4Y/T7+jQPx6dO5vZwvD0sr+Cqq2UoVIrywnoGsbMlPH0+yXn0FQRsEylio6a9
vKdSybLa6fW26sWEua+ZlIHemGFvHQ9XNrlJbSKgM4T9HvC5bs9L6KXsSLNQUcqI
P1Y91PkG+2IkihKiwQJBAPgDqndHjDjdkHDEl59dBnMEF8hUEO0ziu3OjwnPlzv1
j6wNPDmdXq9U7J20FtujdYHBEh2sP5f9nuLH4tRiJqUCQQDPCo7djpQTUZk9hb8t
+4GTPoXnA/NbvMte+nHRiPO47ZC9D1tOjJKZlFuRCY0Meo2wPmO0DUCva2EnUX3s
PrIJAkA0y5r7H0jzRf8cck0QiJ351/I0G+kqhWFatDDw1rcL9X8rEfozDZP9YOep
vo9rHAXEpFP16xfyg/PRtNlNesNdAkAjGr8ugcZJoERDUjIgMcy+kpNRoDHbFB/H
ct9pj7cDXAR2iewJXXxd3fHInb30p7LudyWgmb6l/6bxa7fWHqtBAkAbJuWa15Rn
NgXtScAjuPVNbTIztwpCG+sBp3zZPVitEqrmiUfpcAP7XuuMHhTHn2WVJ0+icZY1
jN0pFHTUdTCS`,
		},
		{
			key: `MIICdQIBADANBgkqhkiG9w0BAQEFAASCAl8wggJbAgEAAoGBAMiVMWPJXP2B9fWKv18CENwPZbJfOasLI9MHurbzs6rCvlXTmiG+hsWcTw8KkoefFK3MfqUBIxnOM5ybai6l7f1ODfF01qzk0FudSbkSQsA87tXZdLbwxEEbbWBNbs4BPBirLvxa7AsVAto0iHEcq2IKLt87h4MYGfP4vZwuSBXNAgMBAAECgYBxVYswnMhEHTiCYsE6x4oLLVAC9zc4Y/T7+jQPx6dO5vZwvD0sr+Cqq2UoVIrywnoGsbMlPH0+yXn0FQRsEylio6a9vKdSybLa6fW26sWEua+ZlIHemGFvHQ9XNrlJbSKgM4T9HvC5bs9L6KXsSLNQUcqIP1Y91PkG+2IkihKiwQJBAPgDqndHjDjdkHDEl59dBnMEF8hUEO0ziu3OjwnPlzv1j6wNPDmdXq9U7J20FtujdYHBEh2sP5f9nuLH4tRiJqUCQQDPCo7djpQTUZk9hb8t+4GTPoXnA/NbvMte+nHRiPO47ZC9D1tOjJKZlFuRCY0Meo2wPmO0DUCva2EnUX3sPrIJAkA0y5r7H0jzRf8cck0QiJ351/I0G+kqhWFatDDw1rcL9X8rEfozDZP9YOepvo9rHAXEpFP16xfyg/PRtNlNesNdAkAjGr8ugcZJoERDUjIgMcy+kpNRoDHbFB/Hct9pj7cDXAR2iewJXXxd3fHInb30p7LudyWgmb6l/6bxa7fWHqtBAkAbJuWa15RnNgXtScAjuPVNbTIztwpCG+sBp3zZPVitEqrmiUfpcAP7XuuMHhTHn2WVJ0+icZY1jN0pFHTUdTCS`,
		},
	}

	for _, test := range testTbl {
		var _, err = ncrypto.DecodePrivateKey([]byte(test.key)).PKCS8().RSAPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecodePublicKey_RSA_PKCS1(t *testing.T) {
	var testTbl = []struct{ key string }{
		{
			key: `-----BEGIN RSA PUBLIC KEY-----
MIGJAoGBALNdX+ylpf3546Mzl/XZbp4NO2zNT1/2uClcbgRDDNkjdYzn4V770Gfo
1BAgt8XWjdn7cETMcwxxjDGtDD+2irajok9yFtQrt4CspzqmX8ZXe4h59l0qdzPl
tm8IUyCImunLvmVgqEmCi1AUR9B36Sza3SC0LTTu46OZj1AMHYRVAgMBAAE=
-----END RSA PUBLIC KEY-----`,
		},
		{
			key: `MIGJAoGBALNdX+ylpf3546Mzl/XZbp4NO2zNT1/2uClcbgRDDNkjdYzn4V770Gfo
1BAgt8XWjdn7cETMcwxxjDGtDD+2irajok9yFtQrt4CspzqmX8ZXe4h59l0qdzPl
tm8IUyCImunLvmVgqEmCi1AUR9B36Sza3SC0LTTu46OZj1AMHYRVAgMBAAE=`,
		},
		{
			key: `MIGJAoGBALNdX+ylpf3546Mzl/XZbp4NO2zNT1/2uClcbgRDDNkjdYzn4V770Gfo1BAgt8XWjdn7cETMcwxxjDGtDD+2irajok9yFtQrt4CspzqmX8ZXe4h59l0qdzPltm8IUyCImunLvmVgqEmCi1AUR9B36Sza3SC0LTTu46OZj1AMHYRVAgMBAAE=`,
		},
	}

	for _, test := range testTbl {
		var _, err = ncrypto.DecodePublicKey([]byte(test.key)).PKCS1().RSAPublicKey()
		if err != nil {
			t.Fatal(err)
		}
	}

}

func TestDecodePublicKey_RSA_PKIX(t *testing.T) {
	var testTbl = []struct{ key string }{
		{
			key: `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCmpOs0IP48kUDUnwequvR6LIcW
qy0LVB+tYwPnt2sjTRrEM1bF/BRzBuS7XGAzw4tjLyvAWKas9pkFZPewoXddJlO3
jmZfQIR9c0J4XapVepo9KnIymdWvJ/DEx1jrmmFGfIZVo2MKxCZTaY6LxnM0S5VI
XnwtFEeeESPB4fZqHQIDAQAB
-----END PUBLIC KEY-----`,
		},
		{
			key: `MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCmpOs0IP48kUDUnwequvR6LIcW
qy0LVB+tYwPnt2sjTRrEM1bF/BRzBuS7XGAzw4tjLyvAWKas9pkFZPewoXddJlO3
jmZfQIR9c0J4XapVepo9KnIymdWvJ/DEx1jrmmFGfIZVo2MKxCZTaY6LxnM0S5VI
XnwtFEeeESPB4fZqHQIDAQAB`,
		},
		{
			key: `MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCmpOs0IP48kUDUnwequvR6LIcWqy0LVB+tYwPnt2sjTRrEM1bF/BRzBuS7XGAzw4tjLyvAWKas9pkFZPewoXddJlO3jmZfQIR9c0J4XapVepo9KnIymdWvJ/DEx1jrmmFGfIZVo2MKxCZTaY6LxnM0S5VIXnwtFEeeESPB4fZqHQIDAQAB`,
		},
	}

	for _, test := range testTbl {
		var _, err = ncrypto.DecodePublicKey([]byte(test.key)).PKIX().RSAPublicKey()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncodePrivateKey_RSA_PKCS1(t *testing.T) {
	var p1, _, _ = ncrypto.GenerateRSAKeyPair(1024)
	if _, err := ncrypto.EncodePrivateKey(p1).PKCS1(); err != nil {
		t.Fatal(err)
	}
}

func TestEncodePrivateKey_RSA_PKCS8(t *testing.T) {
	var p1, _, _ = ncrypto.GenerateRSAKeyPair(1024)
	if _, err := ncrypto.EncodePrivateKey(p1).PKCS8(); err != nil {
		t.Fatal(err)
	}
}

func TestEncodePublicKey_RSA_PKCS1(t *testing.T) {
	var _, p1, _ = ncrypto.GenerateRSAKeyPair(1024)
	if _, err := ncrypto.EncodePublicKey(p1).PKCS1(); err != nil {
		t.Fatal(err)
	}
}

func TestEncodePublicKey_RSA_PKIX(t *testing.T) {
	var _, p1, _ = ncrypto.GenerateRSAKeyPair(1024)
	if _, err := ncrypto.EncodePublicKey(p1).PKIX(); err != nil {
		t.Fatal(err)
	}
}
