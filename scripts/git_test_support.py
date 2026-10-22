"""Environment for disposable Git repositories used by infrastructure tests."""
import os
import subprocess


def git_fixture_environment():
    """Keep ordinary settings, but do not inherit the caller's repository."""
    environment = os.environ.copy()
    names = subprocess.check_output(
        ["git", "rev-parse", "--local-env-vars"], text=True,
    ).splitlines()
    for name in names:
        environment.pop(name, None)
    return environment
