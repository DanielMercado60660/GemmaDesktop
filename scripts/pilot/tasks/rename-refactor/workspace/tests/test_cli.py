import io
import unittest
from unittest import mock

from parcels.cli import main


def run(*argv):
    out = io.StringIO()
    code = main(list(argv), out=out)
    return code, out.getvalue()


class CliTests(unittest.TestCase):
    def test_single_quote(self):
        self.assertEqual(run("2.4", "94103"), (0, "2.4 kg to 94103 (zone 4, standard): $16.30\n"))

    def test_express_table(self):
        code, text = run("1", "--table", "--express")
        self.assertEqual(code, 0)
        self.assertEqual(text.splitlines(), ["zone 1: $7.35", "zone 2: $10.20", "zone 3: $14.44", "zone 4: $20.14"])

    def test_bad_weight_is_an_error(self):
        with mock.patch("sys.stderr", new_callable=io.StringIO):
            self.assertEqual(run("45", "10001"), (2, ""))


if __name__ == "__main__":
    unittest.main()
