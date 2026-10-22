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

    def legacy_services(self):
        directory = self.state / "shared"
        directory.mkdir()
        legacy = {**self.cfg, "postgres": f"n9e-preview-pg-{self.cfg['owner']}",
                  "prometheus": f"n9e-preview-prom-{self.cfg['owner']}"}
        services.write_private(directory / "services.json", legacy)
        return directory, legacy

    def test_legacy_rename_keeps_running_containers_and_persistent_volumes(self):
        directory, legacy = self.legacy_services()
        owned = {"Config": {"Labels": {"n9e.preview.owner": self.cfg["owner"]}}}
        containers = {legacy[key]: owned for key in ("postgres", "prometheus")}

        def rename(command, old, new):
            self.assertEqual(command, "rename")
            containers[new] = containers.pop(old)

        with patch.object(services, "container", side_effect=containers.get), \
                patch.object(services, "docker", side_effect=rename) as docker:
            volumes = services.register_services(directory, self.cfg)
            self.assertEqual(docker.call_count, 2)
            # A second run must retain the old volumes and perform no mutations.
            self.assertEqual(services.register_services(directory, self.cfg), volumes)
            self.assertEqual(docker.call_count, 2)
        self.assertEqual(volumes, {key: legacy[key] + "-data" for key in ("postgres", "prometheus")})
        self.assertIn(self.cfg["postgres"], containers)
        self.assertIn(self.cfg["prometheus"], containers)

    def test_interrupted_rename_can_resume_without_changing_volume_identity(self):
        directory, legacy = self.legacy_services()
        owned = {"Config": {"Labels": {"n9e.preview.owner": self.cfg["owner"]}}}
        containers = {self.cfg["postgres"]: owned, legacy["prometheus"]: owned}
        with patch.object(services, "container", side_effect=containers.get), \
                patch.object(services, "docker") as docker:
            volumes = services.register_services(directory, self.cfg)
        docker.assert_called_once_with("rename", legacy["prometheus"], self.cfg["prometheus"])
        self.assertEqual(volumes["postgres"], legacy["postgres"] + "-data")

    def test_rename_preflights_both_services_before_any_mutation(self):
        directory, legacy = self.legacy_services()
        owned = {"Config": {"Labels": {"n9e.preview.owner": self.cfg["owner"]}}}
        for target in ({"Config": {"Labels": {}}}, owned):
            with self.subTest(target=target):
                containers = {legacy[key]: owned for key in ("postgres", "prometheus")}
                containers[self.cfg["prometheus"]] = target
                with patch.object(services, "container", side_effect=containers.get), \
                        patch.object(services, "docker") as docker:
                    with self.assertRaises(RuntimeError):
                        services.register_services(directory, self.cfg)
                    docker.assert_not_called()
                self.assertEqual(json.loads((directory / "services.json").read_text()), legacy)


if __name__ == "__main__":
    unittest.main()
