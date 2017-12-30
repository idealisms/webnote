import unittest
import os
import sys

LOCAL_APPDATA_PATH = os.getenv('LOCALAPPDATA')
GCLOUD_SDK_PATH = os.path.join(
    LOCAL_APPDATA_PATH, 'Google', 'Cloud SDK', 'google-cloud-sdk')
APPENGINE_PATH = os.path.join(GCLOUD_SDK_PATH, 'platform', 'google_appengine')

TEST_LIBRARY_PATHS = [
    APPENGINE_PATH,
    os.path.join(APPENGINE_PATH, 'lib', 'yaml-3.10'),
    os.path.join(APPENGINE_PATH, 'lib', 'jinja2-2.6'),
    os.path.join(APPENGINE_PATH, 'lib', 'webapp2-2.5.1'),
]


def main():
    sys.path.extend(TEST_LIBRARY_PATHS)

    loader = unittest.TestLoader()
    tests = loader.discover(
        os.path.dirname(os.path.abspath(__file__)),
        '*_test.py')

    runner = unittest.TextTestRunner(verbosity=2)
    runner.run(tests)

if __name__ == '__main__':
    main()
