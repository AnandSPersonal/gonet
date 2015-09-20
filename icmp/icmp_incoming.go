package icmp

import (
	"errors"
	"network/ipv4"
	"network/ipv4/ipv4tps"

	"github.com/hsheth2/logs"
)

type ICMP_Read_Manager struct {
	reader *ipv4.IP_Reader
	buff   map[uint8](chan *ICMP_In)
}

func NewICMP_Read_Manager() (*ICMP_Read_Manager, error) {
	irm := ipv4.GlobalIPReadManager

	ipr, err := ipv4.NewIP_Reader(irm, ipv4tps.IP_ALL, ipv4.ICMP_PROTO)
	if err != nil {
		return nil, err
	}

	x := &ICMP_Read_Manager{
		reader: ipr,
		buff:   make(map[uint8](chan *ICMP_In)),
	}

	go x.readAll()

	return x, nil
}

var GlobalICMPReadManager = func() *ICMP_Read_Manager {
	rm, err := NewICMP_Read_Manager()
	if err != nil {
		logs.Error.Fatal(err)
	}
	return rm
}()

func (x *ICMP_Read_Manager) Bind(ICMP_type uint8) (chan *ICMP_In, error) {
	// add the port if not already there
	if _, found := x.buff[ICMP_type]; !found {
		x.buff[ICMP_type] = make(chan *ICMP_In, ICMP_QUEUE_Size)
	} else {
		return nil, errors.New("Another application binded")
	}
	return x.buff[ICMP_type], nil
}

func (x *ICMP_Read_Manager) Unbind(ICMP_type uint8) error {
	// TODO ICMP unbind
	return nil
}

func (x *ICMP_Read_Manager) readAll() {
	for {
		rip, lip, _, payload, err := x.reader.ReadFrom()
		if err != nil {
			logs.Error.Println(err)
			continue
		}
		////ch logs.Info.Println("Pay", payload, "rip", rip, "lip", lip)

		if len(payload) < ICMP_Header_MinSize {
			//ch logs.Info.Println("Dropping Small ICMP packet:", payload)
			continue
		}

		// extract header
		// TODO verify checksum
		data, err := ExtractICMPHeader(payload, lip, rip)
		if err != nil {
			//ch logs.Info.Println(err)
			continue
		}

		if buf, ok := x.buff[data.Header.TypeF]; ok {
			buf <- data
		} else {
			//ch logs.Info.Println("Dropping ICMP packet:", payload)
		}
	}
}
