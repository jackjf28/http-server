package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipAddr, ok := addr.(*net.IPNet); ok {
				ip := ipAddr.IP
				if !ip.IsLoopback() && ip.To4() != nil {
					return ip.To4(), err
				}
			}
		}
	}
	return []byte{}, nil
}

func sendAsset(fd int, path string) error {
	ext := filepath.Ext(path)
	data, err := os.ReadFile(path)
	if err != nil {
		body := fmt.Sprintf("ERROR: Failed to read file at %v\n", path)
		sendMessage("500", body, "close", fd)
		return errors.New(body)
	}
	ctype, ok := mimeTypes[ext]
	if !ok {
		log.Printf("INFO: Extension: %v not recognized.\n", err)
		ctype = "application/octet-stream"
	}
	data_len := strconv.Itoa(len(data))
	timestamp := time.Now().Format(time.RFC1123)
	var b bytes.Buffer
	b.Write([]byte("HTTP/1.1 200 OK\r\n"))
	b.Write([]byte("Server: Pop! OS\r\n"))
	b.Write([]byte("Date: " + timestamp + "\r\n"))
	b.Write([]byte("Content-Length: " + data_len + "\r\n"))
	b.Write([]byte("Content-Type: " + ctype + "\r\n"))
	b.Write([]byte("Cache-Control: no-store\r\n"))
	b.Write([]byte("Connection: close\r\n"))
	b.Write([]byte("\r\n"))
	b.Write(data)
	b.Write([]byte("\r\n"))
	_, msg_err := syscall.Write(fd, b.Bytes())
	if msg_err != nil {
		if msg_err == syscall.EPIPE {
			return errors.New("Broken pipe: client disconnected before data was sent.")
		} else {
			return msg_err
		}
	}
	return nil
}

func sendMessage(status_code string, body string, conn_status string, client_fd int) error {
	var b bytes.Buffer
	content_len := strconv.Itoa(len(body))
	b.Write([]byte("HTTP/1.1 " + status_code + "\r\n"))
	b.Write([]byte("Content-Type: text/plain\r\n"))
	b.Write([]byte("Content-Length: " + content_len + "\r\n"))
	b.Write([]byte("Connection: " + conn_status + "\r\n"))
	b.Write([]byte("\r\n"))
	b.Write([]byte(body + "\r\n"))

	_, err := syscall.Write(client_fd, b.Bytes())
	if err != nil {
		if err == syscall.EPIPE {
			return errors.New("Broken pipe: client disconnected before data was sent.")
		} else {
			return err
		}
	}
	return nil
}

func readRequest(fd int, recv_buf []byte, n int) error {

	rawRequest := string(recv_buf[:n])
	lines := strings.Split(rawRequest, "\r\n")

	for _, line := range lines {
		switch line {
		case "GET / HTTP/1.1":
			log.Println("INFO: " + line)
			return sendAsset(fd, "./public/index.html")
		case "GET /favicon.ico HTTP/1.1":
			log.Println("INFO: " + line)
			return sendAsset(fd, "./favicon.ico")
		case "GET /css/style.css HTTP/1.1":
			log.Println("INFO: " + line)
			return sendAsset(fd, "./public/css/style.css")
		default:
			log.Println("ERROR: " + line)
			sendMessage("404 NOT FOUND", "Page not found.", "close", fd)
			return errors.New("Page not found.")
		}
	}
	return nil
}

func clientHandler(fd int) {
	defer syscall.Close(fd)
	recv_buf := make([]byte, 1024)
	for {
		n, _, recv_err := syscall.Recvfrom(fd, recv_buf, 0)
		if recv_err != nil {
			log.Printf("ERROR: Recvfrom: %v\n", recv_err)
		}

		if n == 0 {
			body := "Connection closed by peer."
			sendMessage("400", body, "close", fd)
			break
		} else if n < 0 {
			body := "An error occurred."
			sendMessage("400", body, "close", fd)
			break
		} else {
			err := readRequest(fd, recv_buf, n)
			if err != nil {
				log.Printf("ERROR: %v\n", err)
				body := "An error occurred while reading the request. Closing connection"
				sendMessage("400", body, "close", fd)
				break
			}
		}
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
		log.Fatal("ERROR: Error binding socket: ", err)
	}

	log.Printf("INFO: listening on %v:%d...\n", net.IP(sa.Addr[:]).String(), PORT)
	err = syscall.Listen(s, 10)
	if err != nil {
		log.Fatalf("ERROR: Error listening on socket %d: %e", s, err)
	}

	for {
		newfd, their_addr, err := syscall.Accept(s)
		if err != nil {
			log.Fatal(err)
		}
		switch their_addr := their_addr.(type) {
		case *syscall.SockaddrInet4:
			fmt.Printf("INFO: Client Connected: IPv4: %v:%d\n",
				net.IP(their_addr.Addr[:]).String(),
				their_addr.Port)
			// TODO: call client handler here
			go clientHandler(newfd)
		case *syscall.SockaddrInet6:
			fmt.Printf("INFO: Client Connected: IPv6(not supported): %v:%d\n",
				net.IP(their_addr.Addr[:]).String(),
				their_addr.Port)
			continue
		default:
			fmt.Printf("INFO: Unrecognized address type. %t\n", their_addr)
			continue
		}
	}
}
