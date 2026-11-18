package main

import (
	"fmt"
	"github.com/smartwalle/ncrypto/pkcs12"
	"os"
)

func main() {
	data, err := os.ReadFile("./acp_test_sign.pfx")

	if err != nil {
		fmt.Println(err)
		return
	}

	privateKey, certificate, _, err := pkcs12.Decode(data, "000000")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(privateKey)
	fmt.Println(certificate.SerialNumber.String())
}
