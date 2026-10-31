"""Run an assessment-only overlay against a pinned, clean local Sub2API checkout."""
import argparse
import hashlib
import json
import os
import subprocess
from pathlib import Path

PINNED_SOURCE = 'c3a9d9db1e63bc8d09df4571a7e118c2977e187d'
parser = argparse.ArgumentParser()
parser.add_argument('--source-root', required=True, type=Path)
parser.add_argument('--output-root', required=True, type=Path)
parser.add_argument('--go', required=True, type=Path)
args = parser.parse_args()
source = args.source_root.resolve()
output = args.output_root.resolve()
tool = args.go.resolve()
if not source.is_dir() or not tool.is_file():
    raise SystemExit('A local checkout and local Go executable are required')
if output.is_relative_to(source) or output.is_relative_to(Path(__file__).resolve().parents[2]):
    raise SystemExit('Keep assessment output outside both source checkouts')
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=source, text=True).strip()
if revision != PINNED_SOURCE:
    raise SystemExit('Reference source does not match the pinned contract revision')
dirty = subprocess.check_output(['git', 'status', '--porcelain'], cwd=source, text=True).strip()
if dirty:
    raise SystemExit('Reference source must be clean; preserve existing work')
output.mkdir(parents=True, exist_ok=True)
template = Path(__file__).with_name('source_contract_test.go.txt').resolve()
overlay = output/'source-contract-overlay.json'
virtual = source/'backend/internal/service/subscription_key_contract_assessment_test.go'
overlay.write_text(json.dumps({'Replace': {str(virtual): str(template)}}), encoding='utf-8')
env = dict(os.environ)
env.update({'GOWORK': 'off', 'GOMAXPROCS': '4'})
command = [str(tool), 'test', '-p=2', '-tags=unit', '-overlay='+str(overlay), './internal/service',
           '-run', '^TestSubscriptionAndKeyIndependentAmountsCompatibility$', '-timeout=90s', '-count=1', '-v']
with (output/'source-contract.log').open('w', encoding='utf-8') as log:
    result = subprocess.run(command, cwd=source/'backend', env=env, stdout=log, stderr=subprocess.STDOUT, timeout=600)
content = (output/'source-contract.log').read_text(encoding='utf-8')
source_files = ['backend/internal/service/ingress_billing.go', 'backend/internal/service/gateway_usage_billing.go']
record = {'source_revision': revision, 'exit_code': result.returncode, 'reference_source_unchanged': not subprocess.check_output(['git', 'status', '--porcelain'], cwd=source, text=True).strip(),
          'synthetic_only': True, 'production_accessed': False,
          'source_sha256': {name: hashlib.sha256((source/name).read_bytes()).hexdigest() for name in source_files},
          'observations': [line.strip() for line in content.splitlines() if 'subscription_usd=' in line]}
(output/'source-contract-result.json').write_text(json.dumps(record, indent=2))
print(json.dumps(record, indent=2), flush=True)
if result.returncode:
    print(content[-5000:])
raise SystemExit(result.returncode)
