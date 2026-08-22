from importlib.resources import files
import os
import subprocess
import sys

def main() -> None:
    binary = files("git_a2a_bin").joinpath("git-a2a.exe" if os.name == "nt" else "git-a2a")
    argv = [str(binary), *sys.argv[1:]]
    if os.name != "nt":
        os.execv(argv[0], argv)
    raise SystemExit(subprocess.call(argv))
