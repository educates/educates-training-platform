package rest

import (
	"io"
	"net"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The REST API's connections", func() {
	sc := setUpScenario()

	It("are closed when a request's body does not arrive in time", func() {
		connection, err := net.Dial("tcp", strings.TrimPrefix(sc.service.url, "http://"))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = connection.Close() }()

		_, err = connection.Write([]byte("POST /auth/login HTTP/1.1\r\nHost: lookup\r\n" +
			"Content-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"username\":"))
		Expect(err).NotTo(HaveOccurred())

		// The service answers, or not, and then closes the connection; a
		// connection it kept open would hold this read until its deadline.
		Expect(connection.SetReadDeadline(time.Now().Add(20 * time.Second))).To(Succeed())
		_, err = io.Copy(io.Discard, connection)
		Expect(err).NotTo(HaveOccurred(), "the service kept the connection open")
	})
})
