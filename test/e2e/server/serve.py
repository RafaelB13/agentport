#!/usr/bin/env python3
"""Servidor mínimo do harness e2e (roda dentro do container e no host).

Uso: serve.py HOST PORTA CORPO [--tcp]

- Modo padrão: HTTP; qualquer GET responde com CORPO.
- Com --tcp: TCP puro; envia CORPO e fecha a conexão.
- Se HOST contém ':', o socket é IPv6.

Imprime "pronto ..." depois de bindar, para quem quiser esperar pelo listener.
"""

import http.server
import socket
import socketserver
import sys

BODY = b""


class HTTPHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

    def log_message(self, *args):
        pass


class TCPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.sendall(BODY)


class HTTPServer(http.server.ThreadingHTTPServer):
    pass


class TCPServer(socketserver.ThreadingTCPServer):
    pass


def main(argv):
    if len(argv) < 4:
        print(__doc__, file=sys.stderr)
        return 2
    global BODY
    host, port, body = argv[1], int(argv[2]), argv[3]
    BODY = (body + "\n").encode()
    tcp = "--tcp" in argv[4:]
    server_cls, handler_cls = (TCPServer, TCPHandler) if tcp else (HTTPServer, HTTPHandler)
    server_cls.address_family = socket.AF_INET6 if ":" in host else socket.AF_INET
    with server_cls((host, port), handler_cls) as srv:
        print(f"pronto {host}:{port} {'tcp' if tcp else 'http'}", flush=True)
        srv.serve_forever()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
