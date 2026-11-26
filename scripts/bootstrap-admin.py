"""Create the first administrator through the token-protected setup endpoint."""
import argparse
import getpass
import json
from pathlib import Path
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument("--url", default="http://127.0.0.1:8080")
args = parser.parse_args()
token = (Path(__file__).resolve().parents[1] / "deploy/secrets/bootstrap-token").read_text().strip()
body = {"name": input("管理员姓名："), "phone": input("手机号："), "password": getpass.getpass("初始密码：")}
request = urllib.request.Request(args.url + "/api/v1/setup", data=json.dumps(body).encode(), headers={"Content-Type": "application/json", "X-Yihe-Desktop": token}, method="POST")
with urllib.request.urlopen(request, timeout=20) as response:
    result = json.load(response)
if result.get("returnCode") != "1111":
    raise SystemExit("初始化未确认成功，请检查账号是否已存在。")
print("管理员已创建，请在网页登录。")
