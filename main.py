# Copyright 2016 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import webapp2

import constants
import getdates_handler
import getrecent_handler
import load_handler
import save_handler
import string_handler


app = webapp2.WSGIApplication([
    webapp2.Route('/', webapp2.RedirectHandler, defaults={'_uri': '/webnote/'}),
    webapp2.Route('/webnote', webapp2.RedirectHandler, defaults={'_uri': '/webnote/'}),
    webapp2.Route('/webnote/index.html', webapp2.RedirectHandler, defaults={'_uri': '/webnote/'}),

    webapp2.Route(r'/webnote/strings.js', handler=string_handler.StringHandler),
    webapp2.Route(r'/webnote/save.py', handler=save_handler.SaveHandler),
    webapp2.Route(r'/webnote/getrecent.py', handler=getrecent_handler.GetRecentHandler),
    webapp2.Route(r'/webnote/getdates.py', handler=getdates_handler.GetDatesHandler),
    webapp2.Route(r'/webnote/load.py', handler=load_handler.LoadHandler),
    webapp2.Route(r'/webnote/<name:.+>', handler=load_handler.LoadHandler),
], debug=constants.DEBUG)
