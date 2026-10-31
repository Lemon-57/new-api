"""Local Alipay sandbox fixture; no live gateway traffic or merchant secrets.

The application still uses its unmodified SDK, RSA2 verification and TLS checks.
Only its process receives the generated test CA and loopback HTTPS proxy.
"""
import argparse
import base64
import datetime as dt
import hashlib
import http.server
import json
import socketserver
import ssl
import threading
import urllib.parse
from decimal import Decimal
from pathlib import Path

import requests
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

APP_ID = 'local-fixture-app'
SELLER_ID = 'local-fixture-seller'
SANDBOX_HOST = 'openapi-sandbox.dl.alipaydev.com'


def private_key(path):
    if path.exists():
        return serialization.load_pem_private_key(path.read_bytes(), password=None)
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    path.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
    return key


class State:
    def __init__(self, root, callback):
        self.root, self.callback = root, callback
        self.lock = threading.RLock()
        self.merchant = private_key(root/'merchant.private.pem')
        self.ali = private_key(root/'alipay.private.pem')
        self.orders = json.loads((root/'orders.json').read_text()) if (root/'orders.json').exists() else {}

    def save(self):
        temporary = self.root/'orders.pending.json'
        temporary.write_text(json.dumps(self.orders, indent=2))
        temporary.replace(self.root/'orders.json')

    def sign(self, payload):
        return base64.b64encode(self.ali.sign(payload, padding.PKCS1v15(), hashes.SHA256())).decode()

    def trade(self, method, biz):
        order_id = biz.get('out_trade_no', '')
        order = self.orders.get(order_id)
        if method == 'alipay.trade.precreate':
            if not order:
                amount = Decimal(biz['total_amount'])
                if amount <= 0 or amount.quantize(Decimal('.01')) != amount:
                    raise ValueError('invalid amount')
                order = {'out_trade_no': order_id, 'trade_no': 'fixture-'+hashlib.sha256(order_id.encode()).hexdigest()[:24],
                         'total_amount': str(amount), 'trade_status': 'WAIT_BUYER_PAY', 'refunds': {}}
                self.orders[order_id] = order
                self.save()
            if biz['total_amount'] != order['total_amount']:
                raise ValueError('order amount changed')
            if order['trade_status'] != 'WAIT_BUYER_PAY':
                return {'code': '40004', 'sub_code': 'ACQ.TRADE_HAS_SUCCESS'}
            return {'code': '10000', 'out_trade_no': order_id, 'qr_code': 'https://qr.alipay.com/local-fixture-'+order_id}
        if not order:
            return {'code': '40004', 'sub_code': 'ACQ.TRADE_NOT_EXIST'}
        if method == 'alipay.trade.query':
            return {'code': '10000', **{key: order[key] for key in ['out_trade_no', 'trade_no', 'total_amount', 'trade_status']}, 'trans_currency': 'CNY'}
        if method == 'alipay.trade.close':
            if order['trade_status'] != 'WAIT_BUYER_PAY':
                return {'code': '40004', 'sub_code': 'ACQ.TRADE_STATUS_ERROR'}
            order['trade_status'] = 'TRADE_CLOSED'
            self.save()
            return {'code': '10000', 'out_trade_no': order_id, 'trade_no': order['trade_no']}
        if method == 'alipay.trade.refund':
            refund_id = biz['out_request_no']
            amount = Decimal(biz['refund_amount'])
            previous = order['refunds'].get(refund_id)
            if previous is not None and Decimal(previous) != amount:
                raise ValueError('refund identity reused with different amount')
            if previous is None:
                refunded = sum(map(Decimal, order['refunds'].values()), Decimal(0))
                if amount <= 0 or refunded+amount > Decimal(order['total_amount']):
                    raise ValueError('invalid refund amount')
                order['refunds'][refund_id] = str(amount)
                if refunded+amount == Decimal(order['total_amount']):
                    order['trade_status'] = 'TRADE_CLOSED'
                self.save()
            return {'code': '10000', 'out_trade_no': order_id, 'trade_no': order['trade_no'], 'refund_fee': str(amount), 'fund_change': 'N' if previous else 'Y'}
        if method == 'alipay.trade.fastpay.refund.query':
            refund_id = biz['out_request_no']
            amount = order['refunds'].get(refund_id)
            if amount is None:
                return {'code': '10000'}
            return {'code': '10000', 'out_trade_no': order_id, 'trade_no': order['trade_no'], 'out_request_no': refund_id,
                    'refund_amount': amount, 'refund_status': 'REFUND_SUCCESS'}
        raise ValueError('unsupported fixture method')

    def notification(self, order_id):
        order = self.orders[order_id]
        fields = {key: order[key] for key in ['out_trade_no', 'trade_no', 'total_amount', 'trade_status']}
        fields.update({'app_id': APP_ID, 'seller_id': SELLER_ID, 'sign_type': 'RSA2'})
        payload = '&'.join(key+'='+fields[key] for key in sorted(fields) if key != 'sign_type').encode()
        fields['sign'] = self.sign(payload)
        return fields


class Gateway(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        try:
            length = int(self.headers.get('Content-Length', '0'))
            if not 0 < length < 65536:
                raise ValueError('invalid body size')
            form = urllib.parse.parse_qs(self.rfile.read(length).decode(), keep_blank_values=True, strict_parsing=True)
            if any(len(values) != 1 for values in form.values()):
                raise ValueError('duplicate form value')
            form = {key: values[0] for key, values in form.items()}
            if form.get('app_id') != APP_ID:
                raise ValueError('wrong application')
            canonical = '&'.join(key+'='+form[key] for key in sorted(form) if key != 'sign').encode()
            self.server.state.merchant.public_key().verify(base64.b64decode(form['sign']), canonical, padding.PKCS1v15(), hashes.SHA256())
            method = form['method']
            with self.server.state.lock:
                result = self.server.state.trade(method, json.loads(form['biz_content']))
            body = json.dumps(result, separators=(',', ':'), ensure_ascii=False).encode()
            envelope = b'{'+json.dumps(method.replace('.', '_')+'_response').encode()+b':'+body+b',"sign":'+json.dumps(self.server.state.sign(body)).encode()+b'}'
            self.send_response(200)
            self.send_header('Content-Length', str(len(envelope)))
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(envelope)
        except Exception:
            self.send_error(400, 'Fixture request rejected')


class Proxy(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(20)
        headers = b''
        while not headers.endswith(b'\r\n\r\n') and len(headers) < 8192:
            part = self.request.recv(1)
            if not part:
                return
            headers += part
        if not headers.startswith(('CONNECT '+SANDBOX_HOST+':443 HTTP/1.1\r\n').encode()):
            self.request.sendall(b'HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n')
            return
        self.request.sendall(b'HTTP/1.1 200 Connection established\r\n\r\n')
        try:
            with self.server.tls.wrap_socket(self.request, server_side=True) as connection:
                Gateway(connection, self.client_address, self.server)
        except (OSError, ssl.SSLError):
            pass


class Control(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, value):
        data = json.dumps(value).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        with self.server.state.lock:
            self.reply(self.server.state.orders)

    def do_POST(self):
        if self.path != '/paid':
            self.send_error(404)
            return
        length = int(self.headers.get('Content-Length', '0'))
        if not 0 < length < 8192:
            self.send_error(400)
            return
        request = json.loads(self.rfile.read(length))
        with self.server.state.lock:
            order = self.server.state.orders[request['order_id']]
            if order['trade_status'] == 'TRADE_CLOSED':
                self.send_error(409)
                return
            order['trade_status'] = 'TRADE_SUCCESS'
            self.server.state.save()
            notification = self.server.state.notification(request['order_id'])
        status = None
        if request.get('notify', True):
            session = requests.Session()
            session.trust_env = False
            response = session.post(self.server.state.callback, data=notification, timeout=25)
            status = response.status_code
        self.reply({'fixture_paid': True, 'callback_status': status})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--runtime-dir', required=True, type=Path)
    args = parser.parse_args()
    root = args.runtime_dir.resolve()
    if root.is_relative_to(Path(__file__).resolve().parents[2]):
        raise SystemExit('Runtime credentials must stay outside the source repository')
    root.mkdir(parents=True, exist_ok=True)
    state = State(root, 'http://127.0.0.1:47850/api/user/alipay/notify')
    ca_key = private_key(root/'ca.private.pem')
    server_key = private_key(root/'server.private.pem')
    if not (root/'ca.pem').exists():
        now = dt.datetime.now(dt.timezone.utc)
        name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'Local Alipay Fixture CA')])
        ca = x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(ca_key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-dt.timedelta(minutes=5)).not_valid_after(now+dt.timedelta(days=7)).add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True).sign(ca_key, hashes.SHA256())
        leaf_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, SANDBOX_HOST)])
        leaf = x509.CertificateBuilder().subject_name(leaf_name).issuer_name(name).public_key(server_key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-dt.timedelta(minutes=5)).not_valid_after(now+dt.timedelta(days=7)).add_extension(x509.SubjectAlternativeName([x509.DNSName(SANDBOX_HOST)]), critical=False).add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False).sign(ca_key, hashes.SHA256())
        (root/'ca.pem').write_bytes(ca.public_bytes(serialization.Encoding.PEM))
        (root/'server.pem').write_bytes(leaf.public_bytes(serialization.Encoding.PEM))
    config = {'enabled': True, 'sandbox': True, 'appId': APP_ID, 'sellerId': SELLER_ID, 'cnyPerUsd': '7.2',
              'privateKey': state.merchant.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).decode(),
              'publicKey': state.ali.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo).decode(),
              'notifyUrl': state.callback}
    (root/'alipay.private.json').write_text(json.dumps(config))
    # The disposable QEMU guest reaches the workstation through 10.0.2.2.
    # Its egress ACL permits only this pinned fixture port; the control API
    # remains loopback-only.
    proxy = socketserver.ThreadingTCPServer(('0.0.0.0', 47961), Proxy)
    proxy.daemon_threads = True
    proxy.state = state
    proxy.tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    proxy.tls.load_cert_chain(root/'server.pem', root/'server.private.pem')
    control = http.server.ThreadingHTTPServer(('127.0.0.1', 47962), Control)
    control.state = state
    threading.Thread(target=proxy.serve_forever, daemon=True).start()
    print(json.dumps({'proxy_port': 47961, 'control_port': 47962, 'fixture_only': True}), flush=True)
    control.serve_forever()


if __name__ == '__main__':
    main()
