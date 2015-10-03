package arp

import (
	"errors"
	"network/ethernet"

	"time"

	"github.com/hsheth2/logs"
)

var (
	read          ethernet.Reader
	ethtp_manager map[ethernet.EtherType](ARP_Protocol_Dealer)
	// TODO add mutex for protection
)

func init() {
	r, err := ethernet.Bind(ethernet.EtherTypeARP)
	if err != nil {
		logs.Error.Fatalln(err)
	}

	read = r
	ethtp_manager = make(map[ethernet.EtherType](ARP_Protocol_Dealer))

	go dealer()
}

func Register(tp ethernet.EtherType, arppd ARP_Protocol_Dealer) error {
	if tp == ethernet.EtherTypeARP {
		return errors.New("ARP Manager: cannot bind to ARP ethertype")
	}
	if _, ok := ethtp_manager[tp]; ok {
		return errors.New("ARP Manager: ethertype already bound to")
	}
	ethtp_manager[tp] = arppd
	return nil
}

// TODO make unregister function

func dealer() {
	for {
		header, err := read.Read()
		if err != nil {
			logs.Error.Println(err)
			continue
		}
		data := header.Packet
		p := parsePacket(data)

		if pd, ok := ethtp_manager[p.ptype]; ok && p.htype == ethernetHType {
			p = parsePacketWithType(data, p, pd)
			//logs.Trace.Println("ARP packet:", packet)
			pd.Add(p.spa, p.sha)
			if p.oper == operationRequest {
				////ch logs.Trace.Println("Got ARP Request")
				if p.tpa.ARPEqual(pd.GetAddress()) {
					reply := &packet{
						htype: p.htype,
						ptype: p.ptype,
						hlen:  p.hlen,
						plen:  p.plen,
						oper:  operationReply,
						sha:   ethernet.ExternalMACAddress,
						spa:   pd.GetAddress(),
						tha:   p.sha,
						tpa:   p.spa,
					}
					rp, err := reply.MarshalPacket()
					if err != nil {
						logs.Warn.Println("MarshalPacket failed; dropping ARP request")
						continue
					}
					_, err = ethernet.WriteSingle(reply.tha, ethernet.EtherTypeARP, rp)
					if err != nil {
						logs.Warn.Println("Failed to send ARP response; dropping request packet")
						continue
					}
					//logs.Trace.Println("Replied to ARP request")
				} else {
					logs.Warn.Println("Ignoring ARP request with a different target protocol address")
					continue
				}
			} else if p.oper == operationReply {
				//logs.Trace.Println("Got ARP Reply")
				// signal is sent in the Add function
			} else {
				logs.Warn.Println("Dropping ARP packet for bad operation")
			}
		}
	}
}

func Request(tp ethernet.EtherType, raddr ARP_Protocol_Address) (*ethernet.MACAddress, error) {
	if pd, ok := ethtp_manager[tp]; ok {
		// prepare request
		requestPacket := &packet{
			htype: ethernetHType,
			ptype: tp,
			hlen:  ethernetHLen,
			plen:  len(raddr.Len()),
			oper:  operationRequest,
			sha:   ethernet.ExternalMACAddress,
			spa:   pd.GetAddress(),
			tha:   ethernet.ExternalBroadcastAddress,
			tpa:   raddr,
		}

		// make request
		request, err := requestPacket.MarshalPacket()
		if err != nil {
			return nil, err
		}

		// send request
		_, err = ethernet.WriteSingle(requestPacket.tha, ethernet.EtherTypeARP, request)
		if err != nil {
			return nil, err
		}

		// register for reply
		reply := pd.GetReplyNotifier().Register(2)
		defer pd.GetReplyNotifier().Unregister(reply)

		// wait for reply
		timeout := time.NewTimer(requestTimeout)
		for {
			select {
			case <-timeout.C:
				return nil, errors.New("ARP request timed out")
			case <-reply:
				// check if entry is there now; otherwise, wait for another reply
				ans, err := pd.Lookup(raddr)
				if err == nil {
					return ans, nil
				}
			}
		}
	} else {
		return nil, errors.New("No ARP_Protocol_Dealer registered for given EtherType")
	}
}
