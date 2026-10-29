"""Check that Helm charts keep producer and consumer identities separate."""

import re
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVICES = ("cart", "orders", "checkout")


def render(service, *settings):
    args = ["helm", "template", service, str(ROOT / "src" / service / "chart"),
            "--namespace", "retailstore", "--set", f"fullnameOverride={service}"]
    for setting in settings:
        args.extend(["--set", setting])
    output = subprocess.run(args, check=True, capture_output=True, text=True).stdout
    return [doc for doc in re.split(r"(?m)^---\s*$", output) if doc.strip()]


def objects(docs, kind):
    return [doc for doc in docs if re.search(rf"(?m)^kind: {kind}$", doc)]


def name(doc):
    match = re.search(r"(?m)^metadata:\s*\n  name: ([^\s]+)", doc)
    if not match:
        raise AssertionError(f"missing metadata.name: {doc}")
    return match.group(1)


def deployment_accounts(docs):
    result = {}
    for doc in objects(docs, "Deployment"):
        account = re.search(r"(?m)^\s+serviceAccountName: ([^\s]+)", doc)
        if not account:
            raise AssertionError(f"missing serviceAccountName: {doc}")
        result[name(doc)] = account.group(1)
    return result


class ServiceAccountChartTests(unittest.TestCase):
    def test_existing_accounts_are_separate_by_default(self):
        for service in SERVICES:
            with self.subTest(service=service):
                docs = render(service)
                self.assertEqual(deployment_accounts(docs), {
                    f"{service}-api": f"{service}-api",
                    f"{service}-worker": f"{service}-worker",
                })
                self.assertEqual(objects(docs, "ServiceAccount"), [])

    def test_chart_can_create_and_override_each_account(self):
        for service in SERVICES:
            with self.subTest(service=service):
                docs = render(service, "apiServiceAccount.create=true",
                              "workerServiceAccount.create=true",
                              "apiServiceAccount.name=custom-api",
                              "workerServiceAccount.name=custom-worker")
                self.assertEqual(deployment_accounts(docs), {
                    f"{service}-api": "custom-api",
                    f"{service}-worker": "custom-worker",
                })
                self.assertEqual({name(doc) for doc in objects(docs, "ServiceAccount")},
                                 {"custom-api", "custom-worker"})

    def test_disabled_component_does_not_create_its_account(self):
        for service in SERVICES:
            for disabled in ("api", "worker"):
                with self.subTest(service=service, disabled=disabled):
                    docs = render(service, f"{disabled}.enabled=false",
                                  "apiServiceAccount.create=true",
                                  "workerServiceAccount.create=true")
                    other = "worker" if disabled == "api" else "api"
                    self.assertEqual({name(doc) for doc in objects(docs, "ServiceAccount")},
                                     {f"{service}-{other}"})
                    self.assertEqual(deployment_accounts(docs), {
                        f"{service}-{other}": f"{service}-{other}"
                    })


if __name__ == "__main__":
    unittest.main()
