package main

import (
	"fmt"
	"log"
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/songgao/water"
)

type TCPState uint

const (
	Listen TCPState = iota
	SynReceived
	Established
	FinWait1
	FinWait2
	CloseWait
	LastAck
	Closing
	TimeWait
	Closed
	SynSent
)

// RFC 793 p.20
//
//	1         2          3          4
//	----------|----------|----------|----------
//		   SND.UNA    SND.NXT    SND.UNA
//								+SND.WND
//
// 1 - old sequence numbers which have been acknowledged
// 2 - sequence numbers of unacknowledged data
// 3 - sequence numbers allowed for new data transmission
// 4 - future sequence numbers which are not yet allowed
type sendSequenceState struct {
	//  SND.UNA - send unacknowledged
	UNA uint32
	// SND.NXT  - send next
	NXT uint32
	// SND.WND  - send window
	WND uint16
	//  SND.UP  - send urgent pointer (not going to use this)
	UP bool
	//  SND.WL1 - segment sequence number used for last window update
	WL1 uint32
	//  SND.WL2 - segment acknowledgment number used for last window update
	WL2 uint32
	//  ISS     - initial send sequence number
	ISS uint32
}

type receiveSequenceState struct {
	NXT uint32
	WND uint16
	UP  bool
	IRS uint32
}

type session struct {
	socket               net.Conn
	closed               bool
	recv                 chan *tcpPacket
	state                TCPState
	sendSequenceState    sendSequenceState
	receiveSequenceState receiveSequenceState
}

type tcpHandle struct {
	sessions map[string]*session
	ifn      *water.Interface
	mac      []byte
	ipv4     []byte
}

type tcpPacket struct {
	transport *layers.TCP
	ip        *layers.IPv4
	link      *layers.Ethernet
}

func (t *tcpHandle) handle(p gopacket.Packet) {
	tcp := p.Layer(layers.LayerTypeTCP).(*layers.TCP)
	ipv4 := p.Layer(layers.LayerTypeIPv4).(*layers.IPv4)
	ethernet := p.Layer(layers.LayerTypeEthernet).(*layers.Ethernet)
	packet := &tcpPacket{
		transport: tcp,
		ip:        ipv4,
		link:      ethernet,
	}
	srcAddr := fmt.Sprintf("%s:%d", packet.ip.SrcIP, packet.transport.SrcPort)
	dstAddr := fmt.Sprintf("%s:%d", packet.ip.DstIP, packet.transport.DstPort)
	fullAddr := fmt.Sprintf("%s->%s", srcAddr, dstAddr)
	if s, ok := t.sessions[fullAddr]; ok && !s.closed {
		s.recv <- packet
		return
	} else if ok && s.closed {
		close(s.recv)
	}

	chann := make(chan *tcpPacket, 128)
	session := &session{
		recv:  chann,
		state: Listen,
	}
	t.sessions[fullAddr] = session
	// var err error
	// // session.socket, err = net.Dial("tcp", dstAddr)
	// // if err != nil {
	// // 	delete(t.sessions, fullAddr)
	// // 	session.close()
	// // 	return
	// // }

	session.recv <- packet
	go func() {
		for packet := range session.recv {
			switch session.state {
			case Listen:
				syn, ack := packet.transport.SYN, packet.transport.ACK
				if !packet.transport.SYN || packet.transport.ACK {
					fmt.Printf("invalid SYN or ACK value for 'Listen' state (ack=%v, syn=%v)\n", syn, ack)
					session.close()
					return
				}

				err := t.sendHandshake(session, packet)
				if err != nil {
					log.Println("error sending packet:", err)
					session.close()
					return
				}

				fmt.Println("syn received///")
				session.state = SynReceived
				continue
			case SynReceived:
				session.state = Established
				fmt.Println("established, their ack number:", packet.transport.Ack, "our nxt:", session.sendSequenceState.NXT)
			default:
				fmt.Println("established, their ack number:", packet.transport.Ack, packet.transport.Seq, string(packet.transport.Payload), "our nxt:", session.sendSequenceState.NXT)
				fmt.Println("more info:", packet.transport.ACK, packet.transport.SYN)
				log.Printf("error: state not handled yet (state=%d)\n", session.state)
			}
		}
	}()
}

func (t *tcpHandle) openSocket(s *session) {
	for {
		var packet [65535]byte
		n, err := s.socket.Read(packet[:])
		if err != nil {
			s.close()
			return
		}

		if n == 0 {
			continue
		}

		// todo
	}
}

func (s *session) close() {
	s.closed = true
	if s.socket == nil {
		return
	}

	s.socket.Close()
}

func (t *tcpHandle) sendHandshake(session *session, packet *tcpPacket) error {
	session.receiveSequenceState.NXT = packet.transport.Seq
	session.receiveSequenceState.WND = packet.transport.Window
	session.receiveSequenceState.IRS = packet.transport.Seq

	session.sendSequenceState.WND = 1024
	session.sendSequenceState.ISS = 12
	session.sendSequenceState.NXT = session.sendSequenceState.ISS
	session.sendSequenceState.UNA = session.sendSequenceState.ISS

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}
	eth := layers.Ethernet{
		SrcMAC:       t.mac,
		DstMAC:       packet.link.SrcMAC,
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := layers.IPv4{
		SrcIP:    t.ipv4,
		DstIP:    packet.ip.SrcIP,
		Protocol: layers.IPProtocolTCP,
		Version:  4,
		TTL:      64,
	}
	tcp := layers.TCP{
		SrcPort: packet.transport.DstPort,
		DstPort: packet.transport.SrcPort,
		SYN:     true,
		ACK:     true,
		Window:  session.sendSequenceState.WND,
		Seq:     session.sendSequenceState.NXT,
		Ack:     session.receiveSequenceState.NXT,
	}
	gopacket.SerializeLayers(buf, opts, &eth, &ip, &tcp)
	n, err := t.ifn.Write(buf.Bytes())
	if err != nil {
		return fmt.Errorf("unable to send buffer: %w", err)
	}

	if n != len(buf.Bytes()) {
		return fmt.Errorf("number of bytes sent (%d) doesn't match the number of serialised bytes (%d)", n, len(buf.Bytes()))
	}

	return nil
}
