package badgerauth

import (
	"context"
	"log"
	"net"
)

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}

	return localAddr.IP.String()
}

func GetAddrs() []string {
	r := &net.Resolver{
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", "headless")
		},
	}
	selfHost := getOutboundIP()

	addrs, err := r.LookupIPAddr(context.TODO(), "auths")
	if err != nil {
		return nil
	}
	res := make([]string, 0)
	for _, addr := range addrs {
		s := addr.IP.String()
		if s == selfHost {
			continue
		}
		res = append(res, s)
	}
	return res
}
