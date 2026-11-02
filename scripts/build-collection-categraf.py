#!/usr/bin/env python3
"""Build the pinned v0.5.18 provider with configuration logging redacted in a disposable checkout."""
import argparse
from pathlib import Path
import subprocess
import tempfile

PIN = "3af14ebe568108c22ea7ee78d1c64becd03f1076"
ROOT = Path(__file__).resolve().parents[1]

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True, help="existing official Categraf git clone containing the pinned commit")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    source = args.source.resolve()
    resolved = subprocess.check_output(["git", "-C", str(source), "rev-parse", "v0.5.18^{commit}"], text=True).strip()
    if resolved != PIN:
        raise RuntimeError("unexpected v0.5.18 source identity")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="n9e-collection-categraf-") as directory:
        archive = subprocess.Popen(["git", "-C", str(source), "archive", PIN], stdout=subprocess.PIPE)
        subprocess.run(["tar", "-x", "-C", directory], stdin=archive.stdout, check=True)
        archive.stdout.close()
        if archive.wait():
            raise RuntimeError("source archive failed")
        subprocess.run(["git", "apply", str(ROOT / "third_party/categraf/v0.5.18-provider-redaction.patch")], cwd=directory, check=True)
        subprocess.run(["go", "build", "-buildvcs=false", "-tags", "no_logs no_prometheus no_traces", "-ldflags",
                        "-s -w -X flashcat.cloud/categraf/config.Version=v0.5.18-" + PIN + "-provider-redact1",
                        "-o", str(args.output.resolve()), "."], cwd=directory, check=True)

if __name__ == "__main__":
    main()
