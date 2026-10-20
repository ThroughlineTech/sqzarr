PROXMOX HEVC RESEARCH PACKAGE
pve2: Intel i5-8350U | pve5: Intel i7-1185G7
20 October 2026

START HERE
Open proxmox-hevc-research.pdf for the complete 26-page report.
Open proxmox-hevc-research.html in a browser for navigation and copyable commands.
Use proxmox-hevc-research.md for editing, searching or handing the research to another agent.

CONTENTS
- proxmox-hevc-research.pdf: full formatted research report.
- proxmox-hevc-research.html: full report with copyable command blocks and linked sources.
- proxmox-hevc-research.md: full report in portable Markdown.
- sources.md: 32 primary source references and version limitations.
- original-research-brief.txt: your original request, observations and constraints.
- SHA256SUMS.txt: checksums for the files above and this README.

MAIN FINDING
The measured 5.5% advantage describes one short, software-decoded, CPU-fed
VAAPI pipeline. It does not establish either GPU's maximum HEVC capability.
Earlier severe slowdowns have strong power/thermal evidence; the remaining
gap still needs a controlled test to identify its cause.

FIRST THREE TESTS
1. Identify actual versions, encoder entrypoint/settings and synchronized
   CPU/GPU engine activity during the original benchmark.
2. Compare software decode/upload with GPU-resident hardware decode using
   identical continuous local input for both arms.
3. Compare validated encoder queue depths 2, 4 and 8 with a fixed frame pool.
Then verify saved-output quality and 20-minute sustained performance.

USING THE COMMANDS
Commands are proposed operator-run tests with prerequisites and placeholders.
They are not a single script to execute wholesale. Read the adjacent
interpretation, quality, risk and rollback notes. Use the HTML or Markdown
version when copying commands; PDF lines may wrap visually.

OPERATIONAL SCOPE
No host tests, package installations, policy changes, reboots or media edits
were performed for this research. Keep CT260 stopped, preserve CT261's sole
ownership of its production IP, use benchmark copies, and restore SQZARR's
exact original queue state. Build any needed binaries on the workstation.
Maintenance experiments are a separate phase in the report.

SOURCE BOUNDARIES
All measured host values come from your supplied brief. FFmpeg 7 source
inspection and upstream documentation do not establish installed behavior;
local version/capability inventory remains necessary. Full references and
limitations appear in the report and sources.md.
