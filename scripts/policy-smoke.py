#!/usr/bin/env python3
"""Verify Unix deployment policy against a Linux binary using isolated Docker fixtures.

Usage: python3 scripts/policy-smoke.py PATH_TO_LINUX_BINARY
The binary must match the local Docker daemon's architecture. No CRM account,
host policy or credential mounts are used. Test containers have no network;
the image build may pull the Alpine base image.
"""
import json
import pathlib
import shlex
import shutil
import subprocess
import sys
import tempfile
import uuid
root = pathlib.Path(__file__).resolve().parents[1]
binary = pathlib.Path(sys.argv[1]).resolve()
image = 'close-policy-smoke:' + uuid.uuid4().hex
(root / 'bin').mkdir(exist_ok=True)
# CLOSE_POLICY is an override decoy; the binary deliberately ignores it.
base = ['docker', 'run', '--rm', '--pull=never', '--network', 'none', '-e', 'CLOSE_BASE_URL=http://127.0.0.1:1/api/v1/', '-e', 'CLOSE_API_KEY=fixture-key', '-e', 'CLOSE_READONLY=0', '-e', 'CLOSE_POLICY=/tmp/allow.json']

def check(args, code, rule=None, setup=None):
    if setup is None:
        command = base + [image] + args
    else:
        command = base + ['--user', '0', '--entrypoint', '/bin/sh', image, '-eu', '-c', setup + '; exec su nobody -s /bin/sh -c ' + shlex.quote(shlex.join(['/usr/local/bin/close'] + args))]
    p = subprocess.run(command, capture_output=True, text=True)
    assert p.returncode == code, (args, p.returncode, p.stdout, p.stderr)
    if rule:
        assert json.loads(p.stderr)['error']['rule'] == rule and (not p.stdout), (args, p.stdout, p.stderr)
    if code == 10:
        error = json.loads(p.stderr)['error']
        assert error['type'] == 'configuration' and 'deployment policy' in error['message'] and not p.stdout, p.stderr
    print(json.dumps({'args': args, 'exit': p.returncode, 'stdout': p.stdout.strip(), 'stderr': p.stderr.strip()}))
with tempfile.TemporaryDirectory(prefix='policy-smoke-', dir=root / 'bin') as directory:
    context = pathlib.Path(directory)
    shutil.copyfile(binary, context / 'close')
    (context / 'close').chmod(493)
    shutil.copyfile(root / 'policies/restricted-agent.json', context / 'policy.json')
    (context / 'Dockerfile').write_text('FROM alpine:3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a\nRUN mkdir -p /etc/close && chmod 0755 /etc/close\nCOPY close /usr/local/bin/close\nCOPY --chmod=0644 policy.json /etc/close/policy.json\nUSER 65534:65534\nENTRYPOINT ["/usr/local/bin/close"]\n')
    try:
        subprocess.run(['docker', 'build', '-t', image, str(context)], check=True)
        check(['version'], 0)
        check(['activity', 'email', 'list', '--readonly=false'], 11, 'message-content')
        check(['task', 'list'], 11, 'message-content')
        check(['task', 'get', 'task_demo'], 11, 'message-content')
        check(['task', 'list', '--filter', '_type=email_followup'], 11, 'message-content')
        check(['task', 'get', 'task_demo', '--fields', 'id'], 11, 'message-content')
        check(['task', 'list', '--api-fields', 'body_preview'], 11, 'message-content')
        check(['task', 'get', 'task_demo', '--api-fields', 'body_preview'], 11, 'message-content')
        check(['task', 'get', 'task_demo', '--api-fields', 'subject,voicemail_url,recording_url,attachments'], 11, 'message-content')
        check(['task', 'list', '--api-fields', 'id,_type,text,date'], 8)
        check(['task', 'get', 'task_demo', '--api-fields', 'id,_type,text,date'], 8)
        check(['lead', 'list'], 11, 'message-content')
        check(['lead', 'get', 'lead_demo'], 11, 'message-content')
        check(['lead', 'get', 'lead_demo', '--api-fields', 'id,tasks'], 11, 'message-content')
        check(['lead', 'get', 'lead_demo', '--api-fields', 'id,name'], 8)
        check(['activity', 'list', '--fields', 'id'], 11, 'message-content')
        check(['api', 'GET', '/me/'], 11, 'raw-api')
        check(['lead', 'delete', 'lead_demo', '--confirm'], 11, 'deletes')
        check(['lead', 'update', 'lead_demo', '--data', '{"name":"Demo"}', '--dry-run'], 0)
        check(['lead', 'list', '--api-fields', 'id,name'], 8)
        check(['activity', 'email', 'list'], 10, setup='chmod 0666 /etc/close/policy.json')
        check(['activity', 'email', 'list'], 10, setup='chmod 0777 /etc/close')
        check(['activity', 'email', 'list'], 10, setup='chown nobody /etc/close/policy.json')
        check(['lead', 'list'], 10, setup="printf '%s' '{invalid' > /etc/close/policy.json")
        check(['version'], 0, setup="printf '%s' '{invalid' > /etc/close/policy.json")
        check(['lead', 'list'], 10, setup='chmod 0777 /etc/close; rm /etc/close/policy.json')
        check(['lead', 'update', 'lead_demo', '--data', '{}', '--readonly=false', '--dry-run'], 11, 'writes', setup='printf \'%s\' \'{"version":1,"deny":["writes"]}\' > /etc/close/policy.json')
        check(['lead', 'list'], 10, setup='mkdir -p /private/etc/close; rm /etc/close/policy.json; rmdir /etc/close; ln -s /private/etc/close /etc/close; ln -s ../../policy.json /etc/close/policy.json; printf \'%s\' \'{"version":1,"deny":[]}\' > /private/policy.json; chmod 0666 /private/policy.json')
        check(['lead', 'list'], 10, setup='rm /etc/close/policy.json; ln -s /etc/close/missing.json /etc/close/policy.json')
        check(['lead', 'list'], 10, setup='printf \'%s\' \'{"version":1,"deny":["writes"],"deny":[]}\' > /etc/close/policy.json')
        check(['activity', 'email', 'list'], 11, 'message-content', setup='mv /etc/close/policy.json /etc/close/real.json; ln -s real.json /etc/close/policy.json')
        check(['activity', 'email', 'list'], 11, 'message-content', setup='mkdir -p /private/etc; mv /etc/close /private/etc/close; ln -s /private/etc/close /etc/close')
        check(['lead', 'list'], 8, setup="printf '%s' '{\"version\":1,\"deny\":[]}' > /etc/close/policy.json")
        print('Linux policy binary smoke: 33 cases passed as uid 65534; no external network')
    finally:
        subprocess.run(['docker', 'image', 'rm', image], check=False, stdout=subprocess.DEVNULL)
