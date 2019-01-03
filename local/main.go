package main

import (
	"encoding/binary"
	"fmt"
	"github.com/ritterhou/stinger/core/codec"
	"github.com/ritterhou/stinger/core/common"
	"github.com/ritterhou/stinger/core/network"
	"github.com/ritterhou/stinger/local/pac"
	"log"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

const localPort = 2680

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

var totalDownload uint64
var totalUpload uint64

// 显示带宽以及流量
func BandwidthTraffic() {
	ticker := time.NewTicker(1 * time.Second)
	lastDownload := totalDownload
	lastUpload := totalUpload
	for range ticker.C {
		t := time.Now()
		now := t.Format("2006-01-02 15:04:05")

		download := totalDownload - lastDownload
		upload := totalUpload - lastUpload
		if upload != 0 && download != 0 {
			fmt.Printf("%s %s ↓ %s ↑", now, common.ByteFormat(download), common.ByteFormat(upload))
			fmt.Printf("    (%s ↓ %s ↑)\n", common.ByteFormat(totalDownload), common.ByteFormat(totalUpload))
		}
		lastDownload = totalDownload
		lastUpload = totalUpload
	}
}

func main() {
	go pac.Start("local/pac/pac.js", 2600)
	go BandwidthTraffic()

	var l net.Listener
	var err error
	var host = "0.0.0.0:" + strconv.Itoa(localPort)

	l, err = net.Listen("tcp", host)
	if err != nil {
		log.Fatal("Error listening:", err)
	}
	defer l.Close()

	log.Println("Listening on " + host + " ...")
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("Error accepting:", err)
			continue
		}

		//log.Printf("Connection established %s -> %s \n", conn.RemoteAddr(), conn.LocalAddr())
		c := network.Connection{Conn: conn}
		go handlerSocks5(c)
	}
}

func handlerSocks5(conn network.Connection) {
	authSocks5(conn)
	remoteConn := connectSocks5(conn)

	//log.Printf("Connect success %s -> %s, %s => %s\n", conn.RemoteAddress(), conn.LocalAddress(), remoteConn.LocalAddress(), remoteConn.RemoteAddress())
	handlerSocks5Data(conn, remoteConn)
}

func authSocks5(conn network.Connection) {
	socksVersion := conn.Read(1)[0]
	if socksVersion != 5 {
		log.Fatal("Socks version should be 5, now is", socksVersion)
	}

	authWaysNum := conn.Read(1)[0]
	authWays := conn.Read(uint32(authWaysNum))
	if !in(byte(0), authWays) {
		log.Fatal("Only support [NO AUTHENTICATION REQUIRED] auth way.")
	}

	conn.Write([]byte{5, 0})
}

func connectSocks5(conn network.Connection) network.Connection {
	socksVersion := conn.Read(1)[0]
	if socksVersion != 5 {
		log.Fatal("Socks version should be 5, now is", socksVersion)
	}

	command := conn.Read(1)[0]
	if command != 1 {
		log.Fatal("Only support [CONNECT] command")
	}

	conn.Read(1) // 保留字

	addrType := conn.Read(1)[0]

	var host string
	switch addrType {
	case 1: // ipv4
		data := conn.Read(4)
		host = fmt.Sprintf("%d.%d.%d.%d", data[0], data[1], data[2], data[3])
	case 3: // 域名
		hostLength := conn.Read(1)[0]
		host = string(conn.Read(uint32(hostLength)))
	default:
		log.Fatal("Not support address type", addrType)
	}

	port := binary.BigEndian.Uint16(conn.Read(2))
	targetAddr := host + ":" + strconv.Itoa(int(port))

	server := "127.0.0.1:26800"
	c, err := net.Dial("tcp", server)
	if err != nil {
		log.Fatal("Can't connect to", server)
	}

	// 首先发送到远程服务器的链接请求
	targetAddrBytes := []byte(targetAddr)
	c.Write([]byte{byte(len(targetAddrBytes))})
	c.Write(targetAddrBytes)

	conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})

	return network.Connection{Conn: c}
}

func handlerSocks5Data(localConn network.Connection, remoteConn network.Connection) {
	go func() {
		for {
			// 浏览器 -> local
			buf := localConn.Read(1024)
			if buf == nil {
				remoteConn.Close()
				break
			}

			buf = codec.Encrypt(buf)
			// 记载本地上传的流量
			atomic.AddUint64(&totalUpload, uint64(len(buf)))
			// local -> server
			remoteConn.WriteWithLength(buf)
		}
	}()

	go func() {
		for {
			// server -> local
			buf := remoteConn.ReadWithLength()
			if buf == nil {
				localConn.Close()
				break
			}
			// 记载本地下载的流量
			atomic.AddUint64(&totalDownload, uint64(len(buf)))

			buf = codec.Decrypt(buf)
			// local -> 浏览器
			localConn.Write(buf)
		}
	}()
}

func in(num byte, list []byte) bool {
	for _, e := range list {
		if e == num {
			return true
		}
	}
	return false
}