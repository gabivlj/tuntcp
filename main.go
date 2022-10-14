package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"runtime"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/songgao/water"
)

// taken from slirpnetstack: https://github.com/cloudflare/slirpnetstack
func joinNetNS(nsPath string, run func()) error {
	ch := make(chan error, 2)
	go func() {
		runtime.LockOSThread()
		_, err := ApplyNS(specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
			Path: nsPath,
		})
		if err != nil {
			runtime.UnlockOSThread()
			ch <- fmt.Errorf("joining net namespace %q: %v", nsPath, err)
			return
		}
		run()
		ch <- nil
	}()
	// Here is a big hack. Avoid restoring netns. Allow golang to
	// reap the thread, by not calling runtime.UnlockOSThread().
	// This will avoid any errors from restoreNS().

	err := <-ch
	return err
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: tun <namespace>")
		return
	}

	addr, _ := net.ParseMAC("e6:b4:39:c8:f9:b5")
	procID := os.Args[1]
	nsPath := fmt.Sprintf("/var/run/netns/%s", procID)
	var ifce *water.Interface
	err := joinNetNS(nsPath, func() {
		ifceObject, err := water.New(water.Config{
			DeviceType: water.TAP,
			PlatformSpecificParams: water.PlatformSpecificParams{
				Name: "tun0",
			},
		})
		if err != nil {
			log.Fatal(err)
		}

		ifce = ifceObject
	})

	if err != nil {
		fmt.Println("error joining netspace:", err)
		return
	}

	handle := tcpHandle{
		sessions: map[string]*session{},
		mac:      addr,
		ipv4:     net.IPv4(10, 0, 2, 15),
		ifn:      ifce,
	}
	packet := [65535 * 2]byte{}
	for {
		n, err := ifce.Read(packet[:])
		if err != nil {
			log.Fatal(err)
		}

		p := packet[:n]
		packet := gopacket.NewPacket(p, layers.LinkTypeEthernet, gopacket.NoCopy)
		possibleTcpPacket := packet.Layer(layers.LayerTypeTCP)
		if possibleTcpPacket != nil {
			handle.handle(packet)
			continue
		}

		possibleArpLayer := packet.Layer(layers.LayerTypeARP)
		if possibleArpLayer == nil {
			continue
		}

		arpLayer := possibleArpLayer.(*layers.ARP)
		if arpLayer.Operation != layers.ARPRequest {
			log.Println("we received a random reply, ignoring")
			continue
		}

		fmt.Printf("arp request coming from: %s : %s requests %s\n", net.HardwareAddr(arpLayer.SourceHwAddress).String(), net.IP(arpLayer.SourceProtAddress).String(), net.IP(arpLayer.DstProtAddress).String())
		eth := layers.Ethernet{
			SrcMAC:       addr,
			DstMAC:       arpLayer.SourceHwAddress,
			EthernetType: layers.EthernetTypeARP,
		}

		arp := layers.ARP{
			AddrType:          layers.LinkTypeEthernet,
			Protocol:          layers.EthernetTypeIPv4,
			HwAddressSize:     6,
			ProtAddressSize:   4,
			Operation:         layers.ARPReply,
			SourceHwAddress:   []byte(addr),
			SourceProtAddress: arpLayer.DstProtAddress,
			DstHwAddress:      []byte{0, 0, 0, 0, 0, 0},
			DstProtAddress:    arpLayer.SourceProtAddress,
		}

		// Set up buffer and options for serialization.
		buf := gopacket.NewSerializeBuffer()
		opts := gopacket.SerializeOptions{
			FixLengths:       true,
			ComputeChecksums: true,
		}
		gopacket.SerializeLayers(buf, opts, &eth, &arp)
		n, err = ifce.Write(buf.Bytes())
		if err != nil {
			panic(err)
		}

		fmt.Println("answered", n, "bytes")
	}
}
