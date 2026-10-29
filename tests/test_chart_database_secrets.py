"""Render charts to verify database Secret creation and Pod references."""

import base64
import re
import unittest

from test_serviceaccount_charts import name, objects, render


POSTGRES_SERVICES = ("cart", "orders", "checkout")
SERVICES = (*POSTGRES_SERVICES, "catalog", "recommendations")


def secret_path(service):
    return "database.secret" if service in POSTGRES_SERVICES else "app.persistence.secret"


def password_path(service):
    if service in POSTGRES_SERVICES:
        return "database.password"
    return "app.persistence.secret.password"


def expected_keys(service):
    if service in POSTGRES_SERVICES:
        return {"DB_PASSWORD"}
    if service == "catalog":
        return {"RETAIL_CATALOG_PERSISTENCE_USER", "RETAIL_CATALOG_PERSISTENCE_PASSWORD"}
    return {"RETAIL_RECOMMENDATIONS_PERSISTENCE_PASSWORD"}


class DatabaseSecretChartTests(unittest.TestCase):
    def assert_references(self, docs, service, secret_name):
        deployments = objects(docs, "Deployment")
        self.assertEqual(len(deployments), 2 if service in POSTGRES_SERVICES else 1)
        for deployment in deployments:
            with self.subTest(deployment=name(deployment)):
                refs = re.findall(r"- secretRef:\s*\n\s+name: ([^\s]+)", deployment)
                self.assertEqual(refs, [secret_name])
                self.assertNotRegex(deployment, r"(?m)^\s+optional: true$")

    def test_helm_created_secret_and_deployments_use_same_name(self):
        for service in SERVICES:
            with self.subTest(service=service):
                docs = render(service)
                secrets = objects(docs, "Secret")
                self.assertEqual(len(secrets), 1)
                self.assertEqual(name(secrets[0]), f"{service}-db")
                keys = set(re.findall(r"(?m)^  ([A-Z_]+):", secrets[0]))
                self.assertEqual(keys, expected_keys(service))
                self.assert_references(docs, service, name(secrets[0]))

    def test_existing_secret_is_referenced_without_rendering_credentials(self):
        # This marker is a fake credential used only to detect accidental rendering.
        marker = "test-only-password-not-for-deployment"
        encoded = base64.b64encode(marker.encode()).decode()
        for service in SERVICES:
            with self.subTest(service=service):
                path = secret_path(service)
                docs = render(service, f"{path}.create=false", f"{path}.name=existing-db",
                              f"{password_path(service)}={marker}")
                self.assertEqual(objects(docs, "Secret"), [])
                self.assert_references(docs, service, "existing-db")
                self.assertNotIn(marker, "\n".join(docs))
                self.assertNotIn(encoded, "\n".join(docs))

    def test_custom_name_is_used_when_helm_creates_secret(self):
        for service in SERVICES:
            with self.subTest(service=service):
                docs = render(service, f"{secret_path(service)}.name=custom-db")
                self.assertEqual([name(doc) for doc in objects(docs, "Secret")], ["custom-db"])
                self.assert_references(docs, service, "custom-db")

    def test_generated_name_matches_reference_when_name_is_empty(self):
        for service in (*POSTGRES_SERVICES, "recommendations"):
            for create in ("true", "false"):
                with self.subTest(service=service, create=create):
                    path = secret_path(service)
                    docs = render(service, f"{path}.name=", f"{path}.create={create}")
                    self.assert_references(docs, service, f"{service}-db")
                    self.assertEqual([name(doc) for doc in objects(docs, "Secret")],
                                     [f"{service}-db"] if create == "true" else [])

    def test_recommendations_in_memory_does_not_require_database_secret(self):
        for create in ("true", "false"):
            with self.subTest(create=create):
                docs = render("recommendations", "app.persistence.provider=in-memory",
                              f"app.persistence.secret.create={create}")
                self.assertEqual(objects(docs, "Secret"), [])
                for deployment in objects(docs, "Deployment"):
                    self.assertNotIn("secretRef:", deployment)

    def test_catalog_search_can_use_existing_secret_without_mysql(self):
        docs = render("catalog", "app.persistence.provider=in-memory",
                      "app.search.enabled=true", "app.persistence.secret.create=false",
                      "app.persistence.secret.name=existing-search")
        self.assertEqual(objects(docs, "Secret"), [])
        self.assert_references(docs, "catalog", "existing-search")


if __name__ == "__main__":
    unittest.main()
