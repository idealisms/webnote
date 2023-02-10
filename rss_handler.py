import urllib.parse
import webapp2

from lib import PyRSS2Gen
import load_handler
import models

class RssHandler(load_handler.LoadHandler):

    def get(self, **kwargs):
        name = self.get_workspace_name(kwargs)

        workspace = models.Workspace.get_by_wsName(name)
        if workspace is None:
            self.response.status = 404
            return
        notes = workspace.get_notes_list(self.request)

        self.response.headers['Content-Type'] = 'text/xml'

        entries = []
        for note in notes:
            entries.append(PyRSS2Gen.RSSItem(
                title=urllib.parse.unquote(note.get('text', '')).strip().split('\n')[0][:60],
                description=urllib.parse.unquote(note.get('text', '')).strip(),
                guid=PyRSS2Gen.Guid('%s/webnote/%s#%s' % (
                    self.request.host_url,
                    urllib.parse.quote(urllib.parse.quote(workspace.name)),
                    note.get('id', ''),
                ))))

        # Put the recent entries first.
        entries.reverse()

        rss = PyRSS2Gen.RSS2(
                title = urllib.parse.unquote(workspace.name),
                description = 'Webnote RSS feed',
                link = '%s/webnote/%s' % (
                    self.request.host_url,
                    urllib.parse.quote(urllib.parse.quote(workspace.name))),
                lastBuildDate = workspace.time,
                items = entries)

        rss.write_xml(self.response.body_file)
