"""External KEDA must retain control of replicas across Helm upgrades."""
import re
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class ExternalAutoscalingTests(unittest.TestCase):
    def test_worker_replicas_are_omitted_only_when_external_autoscaling_enabled(self):
        for service in ('cart', 'orders', 'checkout'):
            for enabled in ('false', 'true'):
                with self.subTest(service=service, enabled=enabled):
                    rendered = subprocess.run(['helm', 'template', service, str(ROOT/'src'/service/'chart'),
                        '--show-only', 'templates/deployment-worker.yaml', '--set', 'worker.externalAutoscaling='+enabled],
                        check=True, capture_output=True, text=True).stdout
                    self.assertEqual(bool(re.search(r'^  replicas:', rendered, re.M)), enabled == 'false')


if __name__ == '__main__': unittest.main()
