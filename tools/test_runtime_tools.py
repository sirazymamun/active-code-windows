"""Verify the upload-ready runtime inputs without any old executable."""
import tempfile
import unittest
import shutil
from pathlib import Path
from verify_runtime import ensure_runtime, NAME, PART_COUNT

class RuntimeInputTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        original = Path(__file__).resolve().parent.parent / 'source/runtime/parts'
        parts = cls.root / 'source/runtime/parts'
        shutil.copytree(original, parts)
        cls.runtime = ensure_runtime(cls.root)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_parts_assemble_without_exe(self):
        self.assertFalse((self.root / 'active-code.exe').exists())
        self.assertEqual(len(list((self.root/'source/runtime/parts').glob('*.part*'))), PART_COUNT)
        self.assertEqual(ensure_runtime(self.root), self.runtime)

    def test_direct_input_without_exe(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            target = root/'source/runtime'/NAME
            target.parent.mkdir(parents=True)
            shutil.copyfile(self.runtime, target)
            self.assertFalse((root/'active-code.exe').exists())
            self.assertEqual(ensure_runtime(root), target)

    def test_missing_truncated_and_modified_parts(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            parts = root/'source/runtime/parts'
            shutil.copytree(self.root/'source/runtime/parts', parts)
            part = parts/f'{NAME}.part001'
            raw = part.read_bytes()
            part.unlink()
            with self.assertRaisesRegex(ValueError, 'all 4 browser-upload parts'):
                ensure_runtime(root)
            part.write_bytes(raw[:-1])
            with self.assertRaisesRegex(ValueError, 'Wrong part size'):
                ensure_runtime(root)
            part.write_bytes(bytes([raw[0] ^ 1]) + raw[1:])
            with self.assertRaisesRegex(ValueError, 'SHA-256 mismatch'):
                ensure_runtime(root)
            self.assertFalse((root/'source/runtime'/NAME).exists())
            self.assertFalse((root/'source/runtime'/NAME).with_suffix('.zip.assembling').exists())

if __name__ == '__main__':
    unittest.main()
