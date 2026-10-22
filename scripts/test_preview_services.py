"""Resource ownership and retry boundaries, without touching Docker or host data."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import preview_services as services


class ServicesTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.state = Path(self.temp.name)
        self.directory = self.state / "instance"
        self.directory.mkdir()
        self.binding = {"instance": "0123456789abcdef"}
        self.cfg = services.settings(self.state, {})

    def resource(self, **changes):
        value = {"name": "n9ep_0123456789abcdef", "owner": self.cfg["owner"],
                 "password": "test-password", "initialized": True, "provisioned": True, "roleOwned": True}
        value.update(changes)
        services.write_private(self.directory / "postgres.json", value)
        return value

    def test_initialized_database_disappearance_is_not_a_reset(self):
        self.resource()
        with patch.object(services, "ensure", return_value=self.cfg), \
                patch.object(services, "sql", return_value="") as sql:
            with self.assertRaisesRegex(RuntimeError, "missing"):
                services.prepare(self.state, {}, self.binding, self.directory)
        self.assertEqual(sql.call_count, 1)

    def test_destroy_refuses_wrong_resource_or_owner(self):
        for changes in ({"owner": "other"}, {"name": "n9ep_other"}):
            with self.subTest(changes=changes):
                self.resource(**changes)
                with patch.object(services, "sql") as sql:
                    with self.assertRaisesRegex(RuntimeError, "ownership"):
                        services.destroy(self.state, {}, self.binding, self.directory)
                    sql.assert_not_called()

    def test_destroy_drops_only_its_database_and_role_and_is_retriable(self):
        self.resource()
        for existing in ("n9ep_0123456789abcdef", ""):
            with patch.object(services, "sql", side_effect=[existing, "", "n9ep_0123456789abcdef", ""]) as sql:
                services.destroy(self.state, {}, self.binding, self.directory)
            deletion = "\n".join(call.args[1] for call in sql.call_args_list)
            self.assertIn("DROP DATABASE IF EXISTS n9ep_0123456789abcdef", deletion)
            self.assertIn("DROP ROLE n9ep_0123456789abcdef", deletion)
            self.assertNotIn("postgres;", deletion)

    def test_untrusted_instance_identifier_never_reaches_sql(self):
        with self.assertRaises(ValueError):
            services.resource_name({"instance": "x; DROP DATABASE postgres"})

    def test_refresh_keeps_initialized_resource_and_password(self):
        original = self.resource()
        with patch.object(services, "ensure", return_value=self.cfg), \
                patch.object(services, "sql", side_effect=[original["name"], ""]) as sql:
            result = services.prepare(self.state, {}, self.binding, self.directory)
        self.assertEqual(result["password"], original["password"])
        self.assertTrue(result["initialized"])
        self.assertFalse(any("CREATE" in call.args[1] for call in sql.call_args_list))
        self.assertEqual((self.directory / "postgres.json").stat().st_mode & 0o777, 0o600)

    def test_existing_foreign_container_cannot_be_adopted(self):
        with patch.object(services, "container", return_value={"Config": {"Labels": {}}}), \
                patch.object(services, "docker") as docker:
            with self.assertRaisesRegex(RuntimeError, "another"):
                services.ensure_container("test", self.cfg["owner"], [])
            docker.assert_not_called()

    def test_missing_provisioning_receipt_never_adopts_or_deletes_a_database(self):
        self.resource(provisioned=False)
        with patch.object(services, "ensure", return_value=self.cfg), \
                patch.object(services, "sql", return_value="n9ep_0123456789abcdef") as sql:
            with self.assertRaisesRegex(RuntimeError, "receipt"):
                services.prepare(self.state, {}, self.binding, self.directory)
            with self.assertRaisesRegex(RuntimeError, "receipt"):
                services.destroy(self.state, {}, self.binding, self.directory)
            self.assertFalse(any("DROP" in call.args[1] for call in sql.call_args_list))


if __name__ == "__main__":
    unittest.main()
