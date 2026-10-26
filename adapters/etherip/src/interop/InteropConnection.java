// SPDX-License-Identifier: MIT
package interop;

import java.io.InputStream;
import java.io.OutputStream;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.SocketTimeoutException;

import etherip.protocol.Connection;
import etherip.protocol.ProtocolDecoder;
import etherip.protocol.ProtocolEncoder;

/**
 * The transport under etherip's protocol engine: a TCP or UDP socket with
 * the contract's port and timeout.
 *
 * etherip's own TcpConnection and UdpConnection always use port 44818
 * (Connection.port is fixed before their constructors connect), a TCP read
 * waits up to 10 x 2 s, and UdpConnection receives on a second socket bound
 * to local port 44818 with a blocking DatagramChannel, whose receive() has
 * no timeout. Encoding and decoding stay etherip's: write() and read() hand
 * etherip's Connection buffer (600 octets) to the etherip Protocol, exactly
 * as those classes do; only the bytes move through this socket instead.
 */
final class InteropConnection extends Connection
{
    private final Socket tcp;
    private final DatagramSocket udp;
    private final InetSocketAddress target;

    InteropConnection(final String host, final int port, final int timeoutMs, final boolean useUdp)
            throws Exception
    {
        super(host, 0);
        this.port = port;
        this.timeout_ms = timeoutMs;
        this.target = new InetSocketAddress(host, port);
        if (this.target.isUnresolved())
            throw new java.net.UnknownHostException(host);
        if (useUdp)
        {
            this.tcp = null;
            this.udp = new DatagramSocket();
            this.udp.setSoTimeout(timeoutMs);
            this.udp.connect(this.target);
        }
        else
        {
            this.udp = null;
            this.tcp = new Socket();
            this.tcp.setTcpNoDelay(true);
            this.tcp.connect(this.target, timeoutMs);
            this.tcp.setSoTimeout(timeoutMs);
        }
    }

    @Override
    public boolean isOpen()
    {
        return this.tcp != null ? !this.tcp.isClosed() && this.tcp.isConnected() : !this.udp.isClosed();
    }

    @Override
    public void write(final ProtocolEncoder encoder) throws Exception
    {
        this.buffer.clear();
        encoder.encode(this.buffer, null);
        this.buffer.flip();
        final byte[] out = new byte[this.buffer.remaining()];
        this.buffer.get(out);
        if (this.tcp != null)
        {
            final OutputStream os = this.tcp.getOutputStream();
            os.write(out);
            os.flush();
        }
        else
            this.udp.send(new DatagramPacket(out, out.length));
    }

    @Override
    protected void read(final ProtocolDecoder decoder) throws Exception
    {
        this.buffer.clear();
        final long deadline = System.nanoTime() + this.timeout_ms * 1_000_000L;
        final byte[] chunk = new byte[2048];
        do
        {
            if (System.nanoTime() > deadline)
                throw new SocketTimeoutException("no complete reply in " + this.timeout_ms + " ms");
            if (this.tcp != null)
            {
                final InputStream in = this.tcp.getInputStream();
                final int n = in.read(chunk);
                if (n < 0)
                    throw new java.io.EOFException("connection closed by the target");
                this.buffer.put(chunk, 0, n);
            }
            else
            {
                final DatagramPacket p = new DatagramPacket(chunk, chunk.length);
                this.udp.receive(p);
                this.buffer.put(chunk, 0, p.getLength());
            }
        }
        while (this.buffer.position() < decoder.getResponseSize(this.buffer));
        this.buffer.flip();
        decoder.decode(this.buffer, this.buffer.remaining(), null);
    }

    @Override
    public void close() throws Exception
    {
        if (this.tcp != null)
            this.tcp.close();
        if (this.udp != null)
            this.udp.close();
    }
}
