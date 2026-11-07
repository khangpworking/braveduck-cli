// Texts the server sends that the board shows translated: t(err.message),
// t(option), t(source.name). They're listed here so the i18n test finds
// them and vi.json keeps their translations. Keep each exactly as the Go
// code writes it.
import { N_ } from './n'

export const SERVER_TEXTS = [
  // The profile form's choices and suggestions (service.FormOptions).
  N_('Intern'),
  N_('Fresher'),
  N_('Junior'),
  N_('Middle'),
  N_('Senior'),
  N_('Lead / Manager'),
  N_('Remote'),
  N_('Hybrid'),
  N_('On-site'),
  N_('Full-time'),
  N_('Part-time'),
  N_('Contract'),
  N_('Freelance'),
  N_('Internship'),
  N_('Working on Saturdays'),
  N_('Pay not stated ("thỏa thuận")'),
  N_('Vietnamese'),
  N_('English'),
  N_('Japanese'),
  N_('Korean'),
  N_('Chinese'),

  // Source names that aren't a site's own (service.sourceName).
  N_('TopCV application updates'),
  N_('Automatic job rating'),

  // Errors from the settings windows' endpoints.
  N_('your profile\'s "About me" already has text — replace it with this CV?'),
  N_('the CV is over 5 MB'),
  N_('invalid input: the CV is empty or too short'),
  N_('upload a PDF, Word (.docx), Markdown (.md) or HTML CV'),
  N_('no text could be read from this CV (a scanned or image-only PDF has none) — upload it as .docx or .md instead'),
  N_('this PDF is too heavy to read (it unpacks to far more than its size); export it again, or upload a .docx'),
  N_('invalid input: too many deal-breakers'),
  N_('invalid input: pick at least 1 model'),
  N_('invalid input: at most 3 models'),
  N_('invalid input: model id must not be empty'),
  N_('invalid input: duplicate model id'),
  N_('invalid input: default model must be one of the selected models'),
  N_('invalid input: type at least 3 characters'),
  N_('a login window for this site is already open'),
  N_('invalid input: only on a shared server — on your own computer, connect your agent with `braveduck mcp`'),
  N_('missing or bad token — reload the board'),

  // Activity lines with nothing to fill in.
  N_('Location updated'),
  N_('Updated your profile'),
  N_('Stopped following a Facebook group'),
  N_('Made a new key for your AI agent (the old one no longer works)'),
  N_("Removed your AI agent's key"),
]
