"""Exercise inference and settlement in a disposable, loopback-only VM clone.

Run only in the existing local disposable Linux VM, as root. Feed synthetic
fixture login data through stdin; never use production accounts or databases.
The original application and browser sessions remain running and unchanged.
"""
import base64
import concurrent.futures
import hashlib
import http.server
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import sqlite3
import struct
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import zlib


assert os.geteuid() == 0
assert socket.gethostname() == 'localhost', 'Only the disposable local VM is supported'
accounts = json.load(sys.stdin)
original = Path('/var/lib/docker/alipay-browser')
binary = original / 'new-api-close'
expected_sha = '26e9458626ed0a16d6e54a50df5bd78dd4e9bd52ebb4288dfbfef5bc13ba98d3'
assert hashlib.sha256(binary.read_bytes()).hexdigest() == expected_sha
with socket.socket() as candidate_port:
    candidate_port.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    candidate_port.bind(('127.0.0.1', 38753))
uid = 10053
for entry in Path('/proc').iterdir():
    if entry.name.isdigit():
        try:
            assert entry.stat().st_uid != uid, 'Assessment UID is already in use'
        except FileNotFoundError:
            pass
root = Path('/var/lib/docker') / ('api-assessment-' + secrets.token_hex(5))
root.mkdir(mode=0o700)
with sqlite3.connect('file:' + str(original / 'one-api.db') + '?mode=ro', uri=True) as source:
    with sqlite3.connect(root / 'one-api.db') as target:
        source.backup(target)
os.chown(root, uid, uid)
os.chown(root / 'one-api.db', uid, uid)
os.chmod(root / 'one-api.db', 0o600)
environment = json.loads((original / 'runtime.private.json').read_text())
assert not environment.get('SQL_DSN') and not environment.get('LOG_SQL_DSN')
for name in ['ALIPAY_DIRECT_CONFIG_FILE', 'HTTP_PROXY', 'HTTPS_PROXY', 'SSL_CERT_FILE']:
    environment.pop(name, None)
environment.update(PORT='38753', VERSION='local-api-assessment', GIN_MODE='release',
                   DISABLE_AUTO_CHANNEL_UPDATE='true', NO_PROXY='127.0.0.1,localhost',
                   SESSION_SECRET=secrets.token_urlsafe(32))

usage = {'prompt_tokens': 103, 'completion_tokens': 17, 'total_tokens': 120,
         'prompt_tokens_details': {'cached_tokens': 41}}
counts = {}
png_chunks = [(b'IHDR', struct.pack('>IIBBBBB', 1, 1, 8, 6, 0, 0, 0)),
              (b'IDAT', zlib.compress(b'\x00\x11\x22\x33\xff')), (b'IEND', b'')]
png_bytes = b'\x89PNG\r\n\x1a\n' + b''.join(struct.pack('>I', len(data)) + kind + data +
    struct.pack('>I', zlib.crc32(kind + data)) for kind, data in png_chunks)
png = base64.b64encode(png_bytes).decode()


class Provider(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        request = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        model = request['model']
        counts[model] = counts.get(model, 0) + 1
        if model.endswith('-fail'):
            self.send_response(400)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'error': {'message': 'Synthetic rejected request',
                                                  'type': 'invalid_request_error', 'code': 'fixture_rejected'}}).encode())
            return
        if self.path == '/v1/images/generations':
            body = {'created': 1710000000, 'data': [{'b64_json': png}]}
        elif self.path == '/v1/chat/completions':
            body = {'id': 'chatcmpl-fixture', 'object': 'chat.completion', 'created': 1710000000,
                    'model': model, 'choices': [{'index': 0, 'message': {'role': 'assistant',
                    'content': 'api-assessment-ok', 'tool_calls': [{'id': 'call_fixture', 'type': 'function',
                    'function': {'name': 'lookup', 'arguments': '{"q":"x"}'}}]},
                    'finish_reason': 'tool_calls'}], 'usage': usage}
        elif self.path == '/v1/responses':
            body = {'id': 'resp_fixture', 'object': 'response', 'status': 'completed',
                    'created_at': 1710000000, 'model': model,
                    'output': [{'type': 'message', 'id': 'msg_fixture', 'role': 'assistant', 'status': 'completed',
                                'content': [{'type': 'output_text', 'text': 'api-assessment-ok', 'annotations': []}]},
                               {'type': 'function_call', 'id': 'fc_fixture', 'call_id': 'call_fixture',
                                'name': 'lookup', 'arguments': '{"q":"x"}', 'status': 'completed'}],
                    'usage': {'input_tokens': 103, 'output_tokens': 17, 'total_tokens': 120,
                              'input_tokens_details': {'cached_tokens': 41}}}
        else:
            self.send_error(404)
            return
        stream = request.get('stream', False)
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream' if stream else 'application/json')
        self.end_headers()
        if not stream:
            self.wfile.write(json.dumps(body).encode())
        elif self.path == '/v1/chat/completions':
            for choices, chunk_usage in [([{'index': 0, 'delta': body['choices'][0]['message'], 'finish_reason': None}], None),
                                         ([{'index': 0, 'delta': {}, 'finish_reason': 'tool_calls'}], None), ([], usage)]:
                chunk = {'id': body['id'], 'object': 'chat.completion.chunk', 'created': 1710000000,
                         'model': model, 'choices': choices}
                if chunk_usage:
                    chunk['usage'] = chunk_usage
                self.wfile.write(('data: ' + json.dumps(chunk) + '\n\n').encode())
            self.wfile.write(b'data: [DONE]\n\n')
        else:
            for event in [{'type': 'response.created', 'response': {'id': body['id'], 'model': model, 'created_at': 1710000000}},
                          {'type': 'response.output_text.delta', 'delta': 'api-assessment-ok'},
                          {'type': 'response.completed', 'response': body}]:
                self.wfile.write(('event: ' + event['type'] + '\ndata: ' + json.dumps(event) + '\n\n').encode())
        self.wfile.flush()


provider = http.server.ThreadingHTTPServer(('127.0.0.1', 38966), Provider)
rules = []
pid = None
result = {'scope': 'local synthetic inference and wallet settlement', 'cases': [],
          'production_accessed': False, 'original_instance_changed': False,
          'binary_sha256': expected_sha, 'evidence_directory': str(root)}
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
base = 'http://127.0.0.1:38753'


def call(path, payload=None, token=None, user_id=None, method=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    if user_id:
        headers['New-Api-User'] = str(user_id)
    request = urllib.request.Request(base + path, None if payload is None else json.dumps(payload).encode(),
                                     headers, method=method)
    try:
        with client.open(request, timeout=25) as response:
            return response.status, response.headers.get('Content-Type', ''), response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.headers.get('Content-Type', ''), error.read()


def api(path, payload=None, auth=None, method=None):
    status, _, raw = call(path, payload, *(auth or (None, None)), method=method)
    body = json.loads(raw)
    assert status == 200 and body.get('success'), 'Native API operation rejected: ' + path + ' (' + str(status) + '): ' + str(body.get('message', ''))
    return body.get('data')


def readback(user_id, token_id):
    with sqlite3.connect('file:' + str(root / 'one-api.db') + '?mode=ro', uri=True) as db:
        wallet = db.execute('select quota,used_quota from users where id=?', (user_id,)).fetchone()
        key = db.execute('select remain_quota,used_quota from tokens where id=?', (token_id,)).fetchone()
        logs = db.execute('select quota,prompt_tokens,completion_tokens from logs where user_id=? and type=2 order by id', (user_id,)).fetchall()
    return wallet, key, logs


try:
    for tool, destination in [('iptables', '127.0.0.0/8'), ('ip6tables', '::1')]:
        rule = ['OUTPUT', '-m', 'owner', '--uid-owner', str(uid), '!', '-d', destination, '-j', 'REJECT']
        subprocess.run([tool, '-I', *rule], check=True, capture_output=True)
        rules.append((tool, rule))
    pid = os.fork()
    if pid == 0:
        os.setsid()
        os.setgid(uid)
        os.setuid(uid)
        os.chdir(root)
        descriptor = os.open(str(root / 'runtime.log'), os.O_WRONLY | os.O_CREAT, 0o600)
        os.dup2(descriptor, 1)
        os.dup2(descriptor, 2)
        os.dup2(os.open('/dev/null', os.O_RDONLY), 0)
        env = dict(os.environ)
        env.update(environment)
        for name in ['SQL_DSN', 'LOG_SQL_DSN', 'REDIS_CONN_STRING', 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY']:
            env.pop(name, None)
        os.execve(binary, [str(binary)], env)
    threading.Thread(target=provider.serve_forever, daemon=True).start()
    for _ in range(40):
        try:
            if api('/api/status')['version'] == 'local-api-assessment':
                break
        except (OSError, ValueError):
            pass
        time.sleep(.25)
    else:
        raise AssertionError('Disposable candidate did not become ready')
    login = api('/api/user/login', {'username': accounts['admin_username'], 'password': accounts['admin_password']})
    admin = (login['access_token'], login['user']['id'])
    name, password = 'api_eval_' + secrets.token_hex(4), secrets.token_urlsafe(24)
    api('/api/user/', {'username': name, 'password': password, 'role': 1}, admin)
    login = api('/api/user/login', {'username': name, 'password': password})
    member = (login['access_token'], login['user']['id'])
    user_id = member[1]
    api('/api/user/', {'id': user_id, 'username': name, 'display_name': name, 'quota': 100000000,
                      'role': 1, 'status': 1, 'group': 'default'}, admin, method='PUT')
    api('/api/user/manage', {'id': user_id, 'action': 'add_quota', 'mode': 'override', 'value': 100000000}, admin)
    for key, value in [('QuotaPerUnit', '100000000'), ('GroupRatio', '{"default":0.2}'), ('LogConsumeEnabled', 'true')]:
        api('/api/option/', {'key': key, 'value': value}, admin, method='PUT')
    models = ['api-assessment-native', 'api-assessment-fail', 'api-assessment-image', 'api-assessment-bridge']
    for model in models:
        snapshot = api('/api/option/model_pricing?model=' + model, auth=admin)
        entry = next((item for item in snapshot['entries'] if item['model_name'] == model), None)
        pricing = {'billing_setting.billing_mode': 'tiered_expr',
                   'billing_setting.billing_expr': 'tier("base", p * 0.4 + c * 1.6 + cr * 0.1)'}
        if model.endswith('-image'):
            pricing = {'billing_setting.billing_mode': 'ratio', 'ModelPrice': .01}
        api('/api/option/model_pricing', {'changes': [{'model_name': model,
            'expected_version': entry['version'] if entry else snapshot['empty_version'], 'pricing': pricing}]}, admin, method='PATCH')
    for kind, selected in [(1, models[:3]), (58, [models[3]])]:
        settings = {}
        if kind == 58:
            settings = {'advanced_custom': {'advanced_routes': [{'incoming_path': '/v1/responses',
                'upstream_path': '/v1/chat/completions', 'converter': 'openai_responses_to_openai_chat_completions'}]}}
        api('/api/channel/', {'mode': 'single', 'channel': {'type': kind, 'key': 'synthetic-upstream-only',
            'name': 'Local API assessment ' + str(kind), 'status': 1, 'base_url': 'http://127.0.0.1:38966',
            'models': ','.join(selected), 'group': 'default', 'priority': 100, 'weight': 1, 'auto_ban': 0,
            'settings': json.dumps(settings)}}, admin)
    api('/api/token/', {'name': 'local-api-assessment', 'remain_quota': 100000000, 'unlimited_quota': False,
                       'expired_time': -1, 'group': 'default'}, member)
    items = api('/api/token/?p=1&page_size=100', auth=member)['items']
    token_id = next(item['id'] for item in items if item['name'] == 'local-api-assessment')
    key = api('/api/token/' + str(token_id) + '/key', {}, member)['key']
    for endpoint, model in [('/v1/chat/completions', models[0]), ('/v1/responses', models[0]), ('/v1/responses', models[3])]:
        for stream in [False, True]:
            before = readback(user_id, token_id)
            payload = {'model': model, 'stream': stream}
            payload.update({'messages': [{'role': 'user', 'content': 'Synthetic assessment'}], 'max_tokens': 128}
                           if endpoint.endswith('completions') else {'input': 'Synthetic assessment', 'max_output_tokens': 128})
            code, content_type, raw = call(endpoint, payload, key)
            if code != 200:
                error = json.loads(raw).get('error', {})
                result['request_failure'] = {'http': code, 'type': error.get('type'), 'message': error.get('message')}
            assert code == 200 and b'api-assessment-ok' in raw and b'lookup' in raw and b'call_fixture' in raw, 'Incomplete inference response'
            if stream:
                assert 'text/event-stream' in content_type and (b'response.completed' in raw if endpoint.endswith('responses') else b'[DONE]' in raw)
            else:
                body = json.loads(raw)
                assert (body.get('status') == 'completed' and body.get('output')) if endpoint.endswith('responses') else body.get('choices')
            for _ in range(30):
                after = readback(user_id, token_id)
                if len(after[2]) == len(before[2]) + 1:
                    break
                time.sleep(.1)
            wallet_delta, token_delta = before[0][0] - after[0][0], before[1][0] - after[1][0]
            record = {'endpoint': endpoint, 'model': model, 'stream': stream, 'http': code,
                      'valid_text_and_tool_call': True, 'wallet_delta': wallet_delta, 'key_delta': token_delta,
                      'new_ledger_rows': len(after[2]) - len(before[2]), 'expected_charge': 1122}
            result['cases'].append(record)
            assert wallet_delta == token_delta == 1122 and after[2][-1] == (1122, 103, 17) and len(after[2]) == len(before[2]) + 1, 'Settlement differs from configured price or duplicates ledger'
    before = readback(user_id, token_id)
    payload = {'model': models[0], 'input': 'Concurrent synthetic assessment', 'max_output_tokens': 128}
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        replies = list(pool.map(lambda _: call('/v1/responses', payload, key), range(4)))
    assert all(code == 200 and b'api-assessment-ok' in raw for code, _, raw in replies)
    for _ in range(30):
        after = readback(user_id, token_id)
        if len(after[2]) == len(before[2]) + 4:
            break
        time.sleep(.1)
    record = {'case': 'four_concurrent_calls', 'successful_requests': 4,
              'wallet_delta': before[0][0] - after[0][0], 'key_delta': before[1][0] - after[1][0],
              'new_ledger_rows': len(after[2]) - len(before[2]), 'expected_charge': 4488}
    result['cases'].append(record)
    assert record['wallet_delta'] == record['key_delta'] == 4488 and record['new_ledger_rows'] == 4
    for label, model, bearer in [('upstream_rejected', models[1], key), ('invalid_key', models[0], 'sk-invalid-synthetic-assessment')]:
        before = readback(user_id, token_id)
        code, _, _ = call('/v1/chat/completions', {'model': model, 'messages': [{'role': 'user', 'content': 'Synthetic failure'}]}, bearer)
        time.sleep(.2)
        after = readback(user_id, token_id)
        record = {'case': label, 'http': code, 'wallet_delta': before[0][0] - after[0][0],
                  'key_delta': before[1][0] - after[1][0], 'new_ledger_rows': len(after[2]) - len(before[2])}
        result['cases'].append(record)
        assert code >= 400 and before == after, 'Rejected request was charged'
    before = readback(user_id, token_id)
    code, _, raw = call('/v1/images/generations', {'model': models[2], 'prompt': 'Synthetic image', 'n': 1}, key)
    assert code == 200 and base64.b64decode(json.loads(raw)['data'][0]['b64_json']) == png_bytes
    for _ in range(30):
        after = readback(user_id, token_id)
        if len(after[2]) == len(before[2]) + 1:
            break
        time.sleep(.1)
    record = {'endpoint': '/v1/images/generations', 'http': code, 'png_returned': True,
              'wallet_delta': before[0][0] - after[0][0], 'key_delta': before[1][0] - after[1][0],
              'new_ledger_rows': len(after[2]) - len(before[2]), 'expected_charge': 200000}
    result['cases'].append(record)
    assert record['wallet_delta'] == record['key_delta'] == 200000 and record['new_ledger_rows'] == 1
    for label in ['key_quota_exhausted', 'wallet_exhausted']:
        api('/api/token/', {'id': token_id, 'name': 'local-api-assessment', 'remain_quota': 0 if label == 'key_quota_exhausted' else 100000000,
                           'expired_time': -1, 'unlimited_quota': False, 'group': 'default'}, member, method='PUT')
        if label == 'wallet_exhausted':
            api('/api/token/?status_only=true', {'id': token_id, 'status': 1}, member, method='PUT')
            api('/api/user/manage', {'id': user_id, 'action': 'add_quota', 'mode': 'override', 'value': 0}, admin)
            active_key = api('/api/token/' + str(token_id), auth=member)
            assert active_key['status'] == 1 and active_key['remain_quota'] > 0
        before = readback(user_id, token_id)
        if label == 'wallet_exhausted':
            assert before[0][0] == 0 and before[1][0] > 0
        else:
            assert before[0][0] > 0 and before[1][0] == 0
        dispatched = sum(counts.values())
        code, _, _ = call('/v1/responses', {'model': models[0], 'input': 'Synthetic quota rejection'}, key)
        time.sleep(.2)
        after = readback(user_id, token_id)
        record = {'case': label, 'http': code, 'wallet_delta': before[0][0] - after[0][0],
                  'key_delta': before[1][0] - after[1][0], 'new_ledger_rows': len(after[2]) - len(before[2]),
                  'upstream_requests': sum(counts.values()) - dispatched}
        result['cases'].append(record)
        assert code >= 400 and before == after and record['upstream_requests'] == 0
        if label == 'wallet_exhausted':
            assert code == 403, 'Wallet rejection must pass the enabled-key admission step'
    result['passed'] = True
finally:
    if pid:
        assert os.readlink('/proc/' + str(pid) + '/exe') == str(binary), 'Unexpected process; preserve it'
        os.kill(pid, signal.SIGTERM)
        os.waitpid(pid, 0)
    provider.shutdown()
    provider.server_close()
    for tool, rule in reversed(rules):
        subprocess.run([tool, '-D', *rule], check=True, capture_output=True)
    result['candidate_stopped'] = True
    (root / 'result.json').write_text(json.dumps(result, indent=2))
    print(json.dumps(result, indent=2), flush=True)
