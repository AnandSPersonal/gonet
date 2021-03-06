package tcp

import (
	"errors"
	"time"

	"github.com/hsheth2/logs"
)

func (c *TCB) packetSender() {
	// TODO: deal with data in urgSend buffers
	c.sendBufferUpdate.L.Lock()
	defer c.sendBufferUpdate.L.Lock()

	for {
		/*logs*/ logs.Trace.Println(c.hash(), "Beginning send with sendBuffer len:", len(c.sendBuffer))
		if len(c.sendBuffer) > 0 {
			sz := uint16(min(uint64(len(c.sendBuffer)), uint64(c.maxSegSize)))
			data := c.sendBuffer[:sz]
			c.sendBuffer = c.sendBuffer[sz:]
			c.sendData(data, len(c.sendBuffer) == 0)
			continue
		}
		c.sendFinished.Broadcast(true)
		if c.stopSending {
			/*logs*/ logs.Trace.Println(c.hash(), "Stopping packet sender; all pending sends have completed")
			return
		}
		c.sendBufferUpdate.Wait()
	}
}

func (c *TCB) sendData(data []byte, push bool) (err error) {
	/*logs*/ logs.Trace.Println(c.hash(), "Sending Data with len:", len(data))
	var flags flag = flagAck
	if push {
		/*logs*/ logs.Trace.Println(c.hash(), "Data send with PSH flag")
		flags |= flagPsh
	}
	pshPacket := &packet{
		header: &header{
			seq:     c.seqNum,
			ack:     c.ackNum,
			flags:   flags,
			urg:     0,
			options: []byte{},
		},
		payload: data,
	}
	c.seqAckMutex.Lock()
	c.seqNum += uint32(len(data))
	c.seqAckMutex.Unlock()
	err = c.sendWithRetransmit(pshPacket)
	if err != nil {
		logs.Error.Println(c.hash(), err)
	}
	return err
}

func (c *TCB) sendWithRetransmit(data *packet) error {
	// send the first packet
	err := c.sendPacket(data)
	if err != nil { // try at least twice
		c.sendPacket(data)
	}

	go func() error {
		// ack listeners
		ackFound := make(chan bool, 1)
		killAckListen := make(chan bool, 1)
		c.listenForAck(ackFound, killAckListen, data.header.seq+data.getPayloadSize())

		// timers and timeouts
		resendTimerChan := make(chan bool, retransmissionLimit)
		timeout := make(chan bool, 1)
		killTimer := make(chan bool, 1)
		resendTimer(resendTimerChan, timeout, killTimer, c.resendDelay)

		// resend if needed
		for {
			select {
			case <-ackFound:
				killTimer <- true
				return nil
			case <-resendTimerChan:
				c.sendPacket(data)
			case <-timeout:
				// TODO deal with a resend timeout fully
				killAckListen <- true
				logs.Error.Println(c.hash(), "Resend of packet seq", data.header.seq, "timed out")
				return errors.New("Resend timed out")
			}
		}
	}()

	return nil
}

func (c *TCB) listenForAck(successOut chan<- bool, end <-chan bool, targetAck uint32) {
	/*logs*/ logs.Trace.Println(c.hash(), "Listening for ack:", targetAck)
	in := c.recentAckUpdate.Register(ackBufferSize)
	go func(in chan interface{}, successOut chan<- bool, end <-chan bool, targetAck uint32) {
		defer c.recentAckUpdate.Unregister(in)
		for {
			select {
			case v := <-in:
				/*logs*/ logs.Trace.Println(c.hash(), "Ack listener got ack: ", v.(uint32))
				if v.(uint32) >= targetAck {
					/*logs*/ logs.Trace.Println(c.hash(), "Killing the resender for ack:", targetAck)
					successOut <- true
					return
				}
			case <-end:
				return
			}
		}
	}(in, successOut, end, targetAck)
}

func resendTimer(timerOutput, timeout chan<- bool, finished <-chan bool, delay time.Duration) {
	for i := 0; i < retransmissionLimit; i++ {
		select {
		case <-time.After(delay):
			timerOutput <- true
			delay *= 2 // increase the delay after each resend
		case <-finished:
			return
		}
	}
	timeout <- true
}

func (c *TCB) sendPacket(d *packet) error {
	// Requires that seq, ack, flags, urg, and options are set
	// Will set everything else

	d.header.srcport = c.lport
	d.header.dstport = c.rport
	c.windowMutex.RLock()
	d.header.window = c.getWindow() // TODO improve the window field calculation
	c.windowMutex.RUnlock()
	d.rip = c.ipAddress
	d.lip = c.srcIP

	pay, err := d.Marshal()
	if err != nil {
		logs.Error.Println(c.hash(), err)
		return err
	}

	n, err := c.writer.WriteTo(pay)
	if n != len(pay) {
		return errors.New("Not all data written successfully")
	}

	if err != nil {
		logs.Error.Println(c.hash(), err)
		return err
	}

	return nil
}

func (c *TCB) sendResetFlag(seq, ack uint32, flag flag) error {
	/*logs*/ logs.Trace.Println(c.hash(), "Sending RST with seq: ", seq, " and ack: ", ack)
	rst := &packet{
		header: &header{
			seq:     seq,
			ack:     ack,
			flags:   flag,
			urg:     0,
			options: []byte{},
		},
		payload: []byte{},
	}

	return c.sendPacket(rst)
}

func (c *TCB) sendReset(seq, ack uint32) error {
	return c.sendResetFlag(seq, ack, flagRst)
}

func (c *TCB) sendAck(seq, ack uint32) error {
	/*logs*/ logs.Trace.Println(c.hash(), "Sending ACK with seq: ", seq, " and ack: ", ack)
	ackPacket := &packet{
		header: &header{
			seq:     seq,
			ack:     ack,
			flags:   flagAck,
			urg:     0,
			options: []byte{},
		},
		payload: []byte{},
	}
	return c.sendPacket(ackPacket)
}

func (c *TCB) sendFin(seq, ack uint32) error {
	/*logs*/ logs.Trace.Println(c.hash(), "Sending FIN with seq: ", seq, " and ack: ", ack)
	finPacket := &packet{
		header: &header{
			seq:     seq,
			ack:     ack,
			flags:   flagAck | flagFin,
			urg:     0,
			options: []byte{},
		},
		payload: []byte{},
	}
	return c.sendPacket(finPacket)
}
