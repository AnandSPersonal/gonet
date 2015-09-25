package ipv4

import (
	"network/ethernet"
	"network/ipv4/arpv4"
	"network/ipv4/ipv4src"
	"network/ipv4/ipv4tps"

	"sync"

	"golang.org/x/net/ipv4"
)

type IP_Writer struct {
	nw          ethernet.Ethernet_Writer
	version     uint8
	dst, src    *ipv4tps.IPaddress
	headerLen   uint16
	ttl         uint8
	protocol    uint8
	identifier  uint16
	idLock      *sync.Mutex
	maxFragSize uint16
}

func NewIP_Writer(dst *ipv4tps.IPaddress, protocol uint8) (*IP_Writer, error) {
	gateway := ipv4src.GlobalSource_IP_Table.Gateway(dst)
	dst_mac, err := arpv4.GlobalARPv4_Table.LookupRequest(gateway)
	if err != nil {
		return nil, err
	}

	// create its own network_writer
	nw, err := ethernet.NewEthernet_Writer(dst_mac, ethernet.ETHERTYPE_IP)
	if err != nil {
		return nil, err
	}

	/*dstIPAddr, err := net.ResolveIPAddr("ip", dst)
	if err != nil {
		//fmt.Println(err)
		return nil, err
	}
	fmt.Println("Full Address: ", dstIPAddr)*/

	/*err = syscall.Connect(fd, addr)
	if err != nil {
		return nil, errors.New("Failed to connect.")
	}*/

	return &IP_Writer{
		//fd:          fd,
		//sockAddr:    addr,
		nw:          nw,
		version:     ipv4.Version,
		headerLen:   IP_HEADER_LEN,
		dst:         dst,
		src:         ipv4src.GlobalSource_IP_Table.Query(dst),
		ttl:         DEFAULT_TTL,
		protocol:    protocol,
		identifier:  20000, // TODO generate this properly
		idLock:      &sync.Mutex{},
		maxFragSize: MTU, // TODO determine this dynamically with LLDP
	}, nil
}

func (ipw *IP_Writer) getID() uint16 {
	ipw.idLock.Lock()
	defer ipw.idLock.Unlock()
	id := ipw.identifier
	ipw.identifier++
	return id
}

func (ipw *IP_Writer) WriteTo(p []byte) (int, error) {
	////ch logs.Trace.Println("IP Preparing to Write:", p)
	//	//ch logs.Info.Println("IPv4 WriteTo request")

	header := make([]byte, ipw.headerLen)
	header[0] = (byte)((ipw.version << 4) + (uint8)(ipw.headerLen/4)) // Version, IHL
	header[1] = 0
	id := ipw.getID()
	header[4] = byte(id >> 8) // Identification
	header[5] = byte(id)
	header[8] = (byte)(ipw.ttl)      // Time to Live
	header[9] = (byte)(ipw.protocol) // Protocol

	// Src and Dst IPs
	//fmt.Println(srcIP)
	//fmt.Println(srcIP[12])
	//fmt.Println(srcIP[13])
	//fmt.Println(srcIP[14])
	//fmt.Println(srcIP[15])
	//fmt.Println(dstIP)
	header[12] = ipw.src.IP[0]
	header[13] = ipw.src.IP[1]
	header[14] = ipw.src.IP[2]
	header[15] = ipw.src.IP[3]
	header[16] = ipw.dst.IP[0]
	header[17] = ipw.dst.IP[1]
	header[18] = ipw.dst.IP[2]
	header[19] = ipw.dst.IP[3]

	maxFragSize := int(ipw.maxFragSize)
	maxPaySize := maxFragSize - int(ipw.headerLen)

	for i := 0; i < len(p)/maxPaySize+1; i += 1 {
		//fmt.Println("Looping fragmenting")
		if len(p) <= maxPaySize*(i+1) {
			header[6] = byte(0)
		} else {
			header[6] = byte(1 << 5) // Flags: May fragment, more fragments
		}
		//fmt.Println("off", i*maxFragSize, byte((i*maxFragSize)>>8), byte(i*maxFragSize))

		offset := (i * maxPaySize) / 8
		//fmt.Println("Header 6 before:", header[6])
		header[6] += byte(offset >> 8)
		//fmt.Println("Header 6 after:", header[6])
		header[7] = byte(offset) // Fragment offset

		totalLen := uint16(0)

		// Payload
		var newPacket []byte
		if len(p) <= maxFragSize*(i+1) {
			newPacket = make([]byte, IP_HEADER_LEN+len(p[maxPaySize*i:]))
			////ch logs.Trace.Println("IP Writing Entire Packet:", p[maxPaySize*i:], "i:", i)
			totalLen = uint16(ipw.headerLen) + uint16(len(p[maxPaySize*i:]))
			//fmt.Println("Full Pack")
			//fmt.Println("len", len(p[maxPaySize*i:]))
			//header[6] = byte(0)

			//fmt.Println("Total Len: ", totalLen)
			header[2] = (byte)(totalLen >> 8) // Total Len
			header[3] = (byte)(totalLen)

			// IPv4 header test (before checksum)
			//fmt.Println("Packet before checksum: ", header)
			// Checksum
			checksum := calculateIPChecksum(header[:20])
			header[10] = byte(checksum >> 8)
			header[11] = byte(checksum)

			copy(newPacket[:IP_HEADER_LEN], header)
			copy(newPacket[IP_HEADER_LEN:], p[maxPaySize*i:])
			////ch logs.Trace.Println("Full Packet to Send in IPv4 Writer:", newPacket, "(len ", len(newPacket), ")")
			//fmt.Println("CALCULATED LEN:", i*maxFragSize+len(p[maxPaySize*i:]))
		} else {
			newPacket = make([]byte, IP_HEADER_LEN+len(p[maxPaySize*i:maxPaySize*(i+1)]))
			////ch logs.Trace.Println("IP Writer Fragmenting Packet")
			totalLen = uint16(ipw.headerLen) + uint16(len(p[maxPaySize*i:maxPaySize*(i+1)]))
			//fmt.Println("Partial packet")
			//fmt.Println("len", len(p[maxPaySize*i:maxPaySize*(i+1)]))

			//fmt.Println("Total Len: ", totalLen)
			header[2] = (byte)(totalLen >> 8) // Total Len
			header[3] = (byte)(totalLen)

			// IPv4 header test (before checksum)
			//fmt.Println("Packet before checksum: ", header)

			// Checksum
			checksum := calculateIPChecksum(header[:20])
			header[10] = byte(checksum >> 8)
			header[11] = byte(checksum)

			copy(newPacket[:IP_HEADER_LEN], header)
			copy(newPacket[IP_HEADER_LEN:], p[maxPaySize*i:maxPaySize*(i+1)])
			////ch logs.Trace.Println("Full Packet Frag to Send in IPv4 Writer:", newPacket, "(len ", len(newPacket), ")")
		}

		// write the bytes
		// //ch logs.Trace.Println("IP Writing:", newPacket)
		_, err := ipw.nw.Write(p)
		if err != nil {
			return 0, err // returning 0 because the fragment is invalid
		}
	}
	//fmt.Println("PAY LEN", len(p))

	return len(p), nil
}

func (ipw *IP_Writer) Close() error {
	return ipw.nw.Close()
}

/* h := &ipv4.Header{
	Version:  ipv4.Version,      // protocol version
	Len:      20,                // header length
	TOS:      0,                 // type-of-service (0 is everything normal)
	TotalLen: len(x) + 20,       // packet total length (octets)
	ID:       0,                 // identification
	Flags:    ipv4.DontFragment, // flags
	FragOff:  0,                 // fragment offset
	TTL:      8,                 // time-to-live (maximum lifespan in seconds)
	Protocol: 17,                // next protocol (17 is UDP)
	Checksum: 0,                 // checksum (apparently autocomputed)
	//Src:    net.IPv4(127, 0, 0, 1), // source address, apparently done automatically
	Dst: net.ParseIP(c.manager.ipAddress), // destination address
	//Options                         // options, extension headers
}
*/
