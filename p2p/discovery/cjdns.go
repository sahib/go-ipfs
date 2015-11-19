package discovery

// mdns introduced in https://github.com/ipfs/go-ipfs/pull/1117

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	// manet "github.com/ipfs/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-multiaddr-net"
	secio "github.com/ipfs/go-ipfs/p2p/crypto/secio"
	"github.com/ipfs/go-ipfs/p2p/host"
	// conn "github.com/ipfs/go-ipfs/p2p/net/conn"
	// transport "github.com/ipfs/go-ipfs/p2p/net/transport"
	// peer "github.com/ipfs/go-ipfs/p2p/peer"
	// swarm "github.com/ipfs/go-ipfs/p2p/net/swarm"

	"github.com/ehmry/go-cjdns/admin"
	// "github.com/ehmry/go-cjdns/key"
	context "github.com/ipfs/go-ipfs/Godeps/_workspace/src/golang.org/x/net/context"
)

type logRW struct {
	n  string
	rw io.ReadWriter
}

func (r *logRW) Read(buf []byte) (int, error) {
	n, err := r.rw.Read(buf)
	if err == nil {
		log.Debugf("%s read: %v", r.n, buf)
	}
	return n, err
}

func (r *logRW) Write(buf []byte) (int, error) {
	log.Debugf("%s write: %v", r.n, buf)
	return r.rw.Write(buf)
}

func (r *logRW) Close() error {
	c, ok := r.rw.(io.Closer)
	if ok {
		return c.Close()
	}
	return nil
}

type cjdnsService struct {
	admin    *admin.Conn
	host     host.Host
	lk       sync.Mutex
	notifees []Notifee
	interval time.Duration
}

func NewCjdnsService(host host.Host, interval time.Duration) (Service, error) {
	cjdnsConfig := &admin.CjdnsAdminConfig{
		Addr:     "127.0.0.1",
		Port:     11234,
		Password: "NONE",
	}
	admin, err := admin.Connect(cjdnsConfig)
	if err != nil {
		log.Error("cjdns connect error: ", err)
		return nil, err
	}

	log.Debug("cjdns admin api connected")

	service := &cjdnsService{
		admin:    admin,
		host:     host,
		interval: interval,
	}

	go func() {
		for {
			service.pollPeerStats()
		}
	}()

	return service, nil
}

func (cjdns *cjdnsService) Close() error {
	return nil
}

func (cjdns *cjdnsService) pollPeerStats() {
	lp := cjdns.host.ID()
	privateKey := cjdns.host.Peerstore().PrivKey(lp)
	sgen := secio.SessionGenerator{LocalID: lp, PrivateKey: privateKey}

	routingTable, err := cjdns.admin.NodeStore_dumpTable()
	if err != nil {
		log.Errorf("cjdns peerstats error: %s", err)
	}

	for _, peer := range routingTable {
		raddr := fmt.Sprintf("[%s]:4001", peer.IP)
		conn, err := net.DialTimeout("tcp", raddr, cjdns.interval)
		if err != nil {
			log.Errorf("cjdns dial error: %s", err)
			continue
		}

		rwc := &logRW{n: "cjdns", rw: conn}

		sess, err := sgen.NewSession(context.TODO(), rwc)
		if err != nil {
			log.Errorf("cjdns dial session error: [%s] %s", raddr, err)
			continue
		}

		rp := sess.RemotePeer().Pretty()
		log.Debugf("possible cjdns peer: /ip6/%s/tcp/4001/ipfs/%s", peer.IP, rp)

		// maddr, err := manet.FromNetAddr(&net.TCPAddr{IP: *peer.IP, Port: 4001})
		// if err != nil {
		// 	log.Errorf("corrupt multiaddr: [%s] %s", peer.IP, err)
		// 	continue
		// }

		// ctx, _ := context.WithTimeout(context.TODO(), time.Second*10)
		// // conn, err := cjdns.host.Network().Swarm().Dial(ctx, maddr, "")
		// dialer := conn.NewDialer(id, cjdns.host.Peerstore().PrivKey(id), nil)
		// dialer.AddDialer(transport.NewTCPTransport())
		// conn, err := dialer.Dial(ctx, maddr, "")
		// if err != nil {
		// 	log.Errorf("cjdns dial error: %s", err)
		// 	continue
		// }
	}
}

func (c *cjdnsService) RegisterNotifee(n Notifee) {
	c.lk.Lock()
	c.notifees = append(c.notifees, n)
	c.lk.Unlock()
}

func (c *cjdnsService) UnregisterNotifee(n Notifee) {
	c.lk.Lock()
	found := -1
	for i, notif := range c.notifees {
		if notif == n {
			found = i
			break
		}
	}
	if found != -1 {
		c.notifees = append(c.notifees[:found], c.notifees[found+1:]...)
	}
	c.lk.Unlock()
}
