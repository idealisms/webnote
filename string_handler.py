import os

import webapp2

class StringHandler(webapp2.RequestHandler):

    CACHE = {}

    @staticmethod
    def getLanguage(request):
        accept_language = request.headers.get('Accept-Language', 'en')
        for token in accept_language.split(','):
            lang = token.split(';')[0]
            lang = lang.strip().lower()
            if lang.startswith('de'):
                return 'de'
            elif lang.startswith('en'):
                return 'en'
        return 'en'


    def get(self):
        lang = self.getLanguage(self.request)

        if lang not in self.CACHE:
            path = os.path.join(os.path.split(__file__)[0], 'strings.js.' + lang)
            with open(path) as f:
                self.CACHE[lang] = f.read()

        self.response.headers['Content-Type'] = 'text/javascript'
        self.response.write(self.CACHE[lang])
