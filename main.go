package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const PORT = 3480

var mimeTypes = map[string]string{
	".html": "text/html",
	".css":  "text/css",
	".ico":  "img/png",
}

func scanForIPv4Address() ([]byte, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []byte{}, err
	}
	for _, i := range interfaces {
		//		fmt.Printf("Found interface: %v\n", i.Name)
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			//			fmt.Printf("  Found address: %v\n", addr.String())
			//			fmt.Printf("	on network: %v\n", addr.Network())
			if ipAddr, ok := addr.(*net.IPNet); ok {
				ip := ipAddr.IP
				if !ip.IsLoopback() && ip.To4() != nil {
					//fmt.Printf("Using %v.\n", addr.String())
					return ip.To4(), err
				}
			}
		}
	}
	return []byte{}, nil
}

func asset_handler(path string) ([]byte, error) {
	ext := filepath.Ext(path)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read file at %v: %v\n", path, err)
	}
	ctype, ok := mimeTypes[ext]
	if !ok {
		log.Printf("Extension: %v not recognized.\n", err)
		ctype = "application/octet-stream"
	}
	var b bytes.Buffer
	b.Write([]byte("HTTP/1.1 200 OK\r\n"))
	b.Write([]byte("Server: Pop! OS\r\n"))
	b.Write([]byte(fmt.Sprintf("Date: %v\r\n", time.Now())))
	b.Write([]byte(fmt.Sprintf("Content-Length: %d\r\n", len(data))))
	b.Write([]byte(fmt.Sprintf("Content-Type: %s\r\n", ctype)))
	b.Write([]byte("Cache-Control: no-store\n"))
	b.Write([]byte("\r\n"))
	b.Write(data)
	return b.Bytes(), nil
}

func clientHandler(fd int) {
	defer syscall.Close(fd)
	recv_buf := make([]byte, 1024)
	// TODO: Handle client disconnects!!!
	n, _, recv_err := syscall.Recvfrom(fd, recv_buf, 0)
	if recv_err != nil {
		log.Fatalf("Recvfrom: %v\n", recv_err)
	}
	rawRequest := string(recv_buf[:n])

	lines := strings.Split(rawRequest, "\r\n")
	fmt.Printf("Request Line: %v\n", lines[0])
	parts := strings.Split(lines[0], " ")
	if len(parts) > 2 && parts[0] == "GET" {
		var data []byte
		path := parts[1]
		filename := strings.TrimPrefix(path, "/")
		fmt.Printf("Requested file: %s\n", filename)
		if strings.HasSuffix(filename, "png") || strings.HasSuffix(filename, "ico") {
			data, _ = asset_handler(filename)
		} else if strings.HasSuffix(filename, "css") {
			data, _ = asset_handler("./public/css/style.css")
		} else if len(filename) == 0 {
			data, _ = asset_handler("./public/index.html")
		}
		sentBytes, err := syscall.Write(fd, data)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Sent bytes: %d\n", sentBytes)
	}
}

func main() {
	var sa syscall.SockaddrInet4
	sa.Port = PORT

	sa_storage, err := scanForIPv4Address()
	sa.Addr = [4]byte(sa_storage)

	if err != nil {
		log.Fatal(err)
	}

	s, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		log.Fatal(err)
	}

	// Set to handle pesky dangling sockets
	err = syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	if err != nil {
		log.Fatal(err)
	}

	err = syscall.Bind(s, &sa)
	if err != nil {
		log.Fatal("Error binding socket: ", err)
	}

	fmt.Printf("listening on %v:%d...\n", net.IP(sa.Addr[:]).String(), PORT)
	err = syscall.Listen(s, 10)
	if err != nil {
		log.Fatalf("Error listening on socket %d: %e", s, err)
	}

	for {
		newfd, their_addr, err := syscall.Accept(s)
		defer syscall.Close(newfd)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("A new fd: %d!\n", newfd)
		switch their_addr := their_addr.(type) {
		case *syscall.SockaddrInet4:
			fmt.Printf("Client Connected: IPv4: %v:%d\n",
				net.IP(their_addr.Addr[:]).String(),
				their_addr.Port)
			// TODO: call client handler here
			clientHandler(newfd)
		case *syscall.SockaddrInet6:
			fmt.Printf("Client Connected: IPv6(not supported): %v:%d\n",
				net.IP(their_addr.Addr[:]).String(),
				their_addr.Port)
			continue
		default:
			fmt.Printf("Unrecognized address type. %t\n", their_addr)
			continue
		}
	}
}
