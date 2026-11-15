import unittest

from release_version import plan_release


class ReleaseVersionTest(unittest.TestCase):
    def test_first_release_uses_source_version(self):
        self.assertEqual(plan_release((1, 2, 8), "head", [], set(), []), ((1, 2, 8), False))

    def test_next_release_uses_largest_stable_version_numerically(self):
        tags = ["v1.2.9", "v1.2.10", "v2.0.0-beta.1", "other"]
        self.assertEqual(plan_release((1, 2, 8), "head", tags, set(), []), ((1, 2, 11), False))
        self.assertEqual(plan_release((1, 3, 0), "head", tags, set(), []), ((1, 3, 0), False))

    def test_repeat_push_skips_published_commit(self):
        published = {"tag_name": "v1.2.8", "prerelease": False, "draft": False, "target_commitish": "main"}
        self.assertEqual(plan_release((1, 2, 8), "head", ["v1.2.8"], {"v1.2.8"}, [published]), (None, False))

    def test_resume_draft_only_for_same_commit(self):
        draft = {"tag_name": "v1.2.9", "prerelease": False, "draft": True, "target_commitish": "head"}
        self.assertEqual(plan_release((1, 2, 8), "head", ["v1.2.8"], set(), [draft]), ((1, 2, 9), True))
        self.assertEqual(plan_release((1, 2, 8), "new-head", ["v1.2.8"], set(), [draft]), ((1, 2, 10), False))
