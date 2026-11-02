"""Verify RDS CA mounts and TLS settings on every API/worker deployment."""

import re
import unittest

from test_serviceaccount_charts import objects, render


SERVICES = ("cart", "orders", "checkout", "catalog", "recommendations")
CA_PATH = "/etc/rds-ca/global-bundle.pem"


class DatabaseTLSChartTests(unittest.TestCase):
    def test_defaults_do_not_require_external_ca(self):
        for service in SERVICES:
            with self.subTest(service=service):
                for deployment in objects(render(service), "Deployment"):
                    self.assertNotIn("/etc/rds-ca", deployment)
                    self.assertNotIn("name: rds-ca", deployment)

    def test_tls_on_every_deployment_even_with_external_app_config(self):
        for service in SERVICES:
            for external_config in (False, True):
                with self.subTest(service=service, external_config=external_config):
                    settings = ["databaseTLS.enabled=true", "databaseTLS.caConfigMapName=custom-rds-ca"]
                    if external_config:
                        settings.append("configMap.create=false")
                    deployments = objects(render(service, *settings), "Deployment")
                    self.assertEqual(len(deployments), 2 if service in SERVICES[:3] else 1)
                    if service in SERVICES[:3]:
                        expected = {"DB_SSLMODE": "verify-full", "DB_SSLROOTCERT": CA_PATH}
                    elif service == "catalog":
                        expected = {"RETAIL_CATALOG_PERSISTENCE_TLS": "true", "SSL_CERT_FILE": CA_PATH}
                    else:
                        expected = {"RETAIL_RECOMMENDATIONS_PERSISTENCE_TLS": "true", "DB_SSL_CA": CA_PATH}
                    for deployment in deployments:
                        self.assertRegex(deployment, r"mountPath: /etc/rds-ca\s+readOnly: true")
                        self.assertRegex(deployment, r'configMap:\s+name: "custom-rds-ca"')
                        self.assertIn("key: global-bundle.pem", deployment)
                        self.assertIn("path: global-bundle.pem", deployment)
                        self.assertNotIn("subPath:", deployment)
                        for variable, value in expected.items():
                            self.assertRegex(deployment, rf'- name: {variable}\s+value: "?{re.escape(value)}"?\s')
                        if service == "catalog":
                            self.assertIn("name: tmp-volume", deployment)
                            self.assertIn("mountPath: /tmp", deployment)


if __name__ == "__main__":
    unittest.main()
