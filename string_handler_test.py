import unittest
import webtest

import string_handler

class StringHandlerTest(unittest.TestCase):

    def testGetLanguage(self):
        test_cases = (
            ('en', 'en-US,en;q=0.9'),
            ('de', 'de,en-US;q=0.9,en;q=0.8'),
            ('en', 'en-GB,de,en;q=0.9'),
            ('de', 'it,de,en-US,en;q=0.9'),
            ('en', 'it;q=0.9'),
            ('en', ''),
            ('en', ';,,,;,,'),
            ('en', ';'),
            ('en', ','),
        )

        for expected, accept_language in test_cases:
            request = webtest.TestRequest({})
            request.headers['Accept-Language'] = accept_language
            self.assertEqual(
                expected, string_handler.StringHandler.getLanguage(request))
