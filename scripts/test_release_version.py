import subprocess
import sys
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("release_version.py")


class ReleaseVersionTest(unittest.TestCase):
    def test_tag_is_the_release_version(self):
        for tag in ("v0.0.0", "v1.2.9", "v10.20.30"):
            with self.subTest(tag=tag):
                result = subprocess.check_output([sys.executable, str(SCRIPT), tag], text=True)
                self.assertEqual(result, f"version={tag[1:]}\n")

    def test_rejects_invalid_tags(self):
        for tag in ("1.2.9", "v1.2", "v01.2.9", "v1.2.9-rc.1", "main"):
            with self.subTest(tag=tag):
                result = subprocess.run([sys.executable, str(SCRIPT), tag], capture_output=True, text=True)
                self.assertEqual(result.returncode, 2)
                self.assertEqual(result.stdout, "")
