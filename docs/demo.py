#!/usr/bin/env python3
"""Draws docs/demo.svg: a split terminal that types out outpost's real output.

Left, `outpost setup` on a new server, gateway included. Right, plain `outpost`:
the menu, a server's status card, and giving a project an HTTPS address.

    python3 docs/demo.py > docs/demo.svg

Each line says when it appears, as a percent of the loop. Copy the text from a
real run when outpost's output changes, so the demo never shows what outpost
doesn't do.
"""

LOOP = 30  # seconds
W, H, PANE, TOP, LINE = 1081, 544, 540, 41, 21
BOTTOM = TOP + 502 - 14  # lines scroll up once they'd pass this

BG, RULE, CARD = "#0E1015", "#262A35", "#3A4152"
TEXT, MUTED, ACCENT, ASK, WARN = "#ECEEF3", "#8B90A0", "#4ADE80", "#7AA2F7", "#F87171"


def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def spans(segs):
    out = []
    for text, color, *bold in segs:
        cls = ' class="b"' if bold else ""
        out.append(f'<tspan fill="{color}"{cls}>{esc(text)}</tspan>')
    return "".join(out)


# Line kinds, as (percent, ...) tuples.
def cmd(t, c):
    return (t, "line", [("$ ", MUTED), (c, TEXT, 1)])


def step(t, n, total, title):
    return (t, "line", [(f"{n}/{total}", ACCENT), (f"  {title}", TEXT, 1)])


def note(t, s):
    return (t, "line", [("    " + s, MUTED)])


def done(t, name, after=""):
    return (t, "line", [("    ✓ ", ACCENT), (name + " ", TEXT), (after, MUTED)])


def ask(t, question):
    return (t, "line", [("    ┃ ", ASK), (question, TEXT, 1)])


def answer(t, a):
    return (t, "line", [("    ┃ ", ASK), ("> ", ACCENT), (a, TEXT)])


def option(t, a, chosen=False):
    return (t, "line", [("    ┃ ", ASK), ("> " if chosen else "  ", ACCENT), (a, TEXT if chosen else MUTED)])


def card(t, title, sub, rows):
    return (t, "card", title, sub, rows)


def boxed(t, first, rest):
    return (t, "box", first, rest)


left = [
    cmd(1, "outpost setup 203.0.113.7"),
    step(3, 1, 6, "SSH key"),
    note(4, "Using ~/.ssh/id_ed25519.pub"),
    step(6, 2, 6, "Connecting to 203.0.113.7"),
    note(7, "Logged in as root"),
    step(9, 3, 6, "Installing (3-5 minutes)"),
    ask(10, "What should this server be called?"),
    answer(12, "devbox"),
    done(13, "Name", "devbox"),
    ask(14, "How many GB of disk should the server use as swap?"),
    answer(16, "4"),
    done(17, "Swap (4 GB)", "3s"),
    done(19, "System packages", "41s"),
    done(20, "Security updates", "4s"),
    done(21, "Node.js", "12s"),
    done(23, "Docker", "38s"),
    done(24, "Tailscale", "9s"),
    done(25, "User and SSH", "2s"),
    done(26, "Firewall", "1s"),
    done(28, "T3 Code and Claude Code", "52s"),
    step(30, 4, 6, "Logins"),
    (31, "line", [("    ◆ ", ASK), ("Tailscale  ", TEXT, 1), ("Open the link and sign in with the same", MUTED)]),
    note(31, "  account as your laptop."),
    done(34, "Tailscale", "signed in"),
    done(36, "Claude", "signed in"),
    done(38, "GitHub", "signed in"),
    done(40, "T3 Connect", "signed in"),
    step(42, 5, 6, "Gateway"),
    ask(43, "Open your projects' web apps from your other devices too?"),
    answer(45, "Yes: give each project an HTTPS address"),
    ask(46, "Which names should your projects get?"),
    answer(48, "Tailscale names, like https://myapp-devbox.tail1234.ts.net"),
    ask(49, "Paste a Tailscale API key"),
    answer(51, "••••••••••••••••••••••••"),
    done(52, "Tailscale API key", "kept on this computer"),
    done(54, "Gateway", "ready: Tailscale names"),
    step(56, 6, 6, "Final check"),
    done(59, "Rebooting", "15s"),
    done(62, "Waiting for the server to come back", "31s"),
    done(64, "T3 Code, Docker and Tailscale running", "3s"),
    boxed(66, [("✓ All set.", ACCENT, 1), (" 203.0.113.7 is ready for your agents.", TEXT)],
          ["In T3 Code, pick this server under Settings → Environments,",
           "add a project with ⌘K → Add Project, and start prompting."]),
]

right = [
    cmd(4, "outpost"),
    ask(6, "Which server?"),
    answer(9, "203.0.113.7"),
    card(12, "devbox · 203.0.113.7", "all good", [
        ("Access", [("T3 Connect", TEXT)]),
        ("Gateway", [("Tailscale names, 0 projects", TEXT)]),
        ("Memory", [("8 GB · 4 GB swap · 70 GB disk free", TEXT)]),
        ("Running", [("✓", ACCENT), (" T3 Code  ", TEXT), ("✓", ACCENT), (" Docker  ", TEXT), ("✓", ACCENT), (" Tailscale  ", TEXT), ("✓", ACCENT), (" Gateway", TEXT)]),
        ("Logins", [("✓", ACCENT), (" Tailscale  ", TEXT), ("✓", ACCENT), (" Claude  ", TEXT), ("✓", ACCENT), (" GitHub", TEXT)]),
    ]),
    (15, "line", [("  ↑↓ to choose, Enter to go, Esc or ← to go back", MUTED)]),
    ask(17, "What would you like to do?"),
    option(17, "Show addresses: T3 Code and each project"),
    option(17, "Access: how devices reach T3 (now T3 Connect)"),
    option(21, "Add a project's address", chosen=True),
    ask(25, "What's the project called?"),
    answer(29, "myapp"),
    ask(32, "Which port does it listen on, on the server?"),
    answer(36, "3000"),
    done(40, "Publishing myapp-devbox.tail1234.ts.net", "2s"),
    done(50, "Checking myapp-devbox.tail1234.ts.net", "18s"),
    (53, "line", [("  ✓ ", ACCENT), ("myapp ", TEXT), ("https://myapp-devbox.tail1234.ts.net", ACCENT)]),
    note(56, "Open it on your laptop or phone: real HTTPS, nothing"),
    note(56, "to install there."),
]


def render(items, x0, clip):
    """Draws one pane's lines, scrolling them up as a terminal would."""
    y, body, starts, scroll = 74, [], set(), [(0, 0)]
    for item in items:
        t, kind = item[0], item[1]
        starts.add(t)
        g = f'<g class="l a{t}">'
        if kind == "line":
            body.append(f'{g}<text x="{x0}" y="{y}">{spans(item[2])}</text></g>')
            bottom = y + 6
            y += LINE
        elif kind == "card":
            _, _, title, sub, rows = item
            top = y - 14
            height = 30 + (len(rows) + 1) * LINE + 10
            parts = [f'<rect x="{x0}" y="{top}" width="500" height="{height}" rx="10" fill="none" stroke="{CARD}"/>',
                     f'<text x="{x0 + 18}" y="{top + 30}">{spans([("▲ ", ACCENT), (title, TEXT, 1), ("  " + sub, ACCENT)])}</text>']
            for i, (label, value) in enumerate(rows):
                ry = top + 30 + (i + 2) * LINE - 6
                parts.append(f'<text x="{x0 + 18}" y="{ry}">{spans([(label.ljust(10), MUTED)] + value)}</text>')
            body.append(g + "".join(parts) + "</g>")
            bottom = top + height
            y = bottom + 26
        elif kind == "box":
            _, _, first, rest = item
            top = y - 14
            height = 24 + len(rest) * LINE + 18
            parts = [f'<rect x="{x0}" y="{top}" width="500" height="{height}" rx="8" fill="none" stroke="{ACCENT}"/>',
                     f'<text x="{x0 + 16}" y="{top + 24}">{spans(first)}</text>']
            for i, line in enumerate(rest):
                parts.append(f'<text x="{x0 + 16}" y="{top + 24 + (i + 1) * LINE}" fill="{MUTED}">{esc(line)}</text>')
            body.append(g + "".join(parts) + "</g>")
            bottom = top + height
            y = bottom + 26
        if bottom - scroll[-1][1] > BOTTOM:
            scroll.append((t, bottom - BOTTOM))
    frames = " ".join(f"{t}% {{ transform: translateY({-o}px) }}" for t, o in scroll)
    style = f"@keyframes {clip} {{ {frames} }} .{clip} {{ animation: {clip} {LOOP}s infinite step-end; }}"
    cursor = f'<g class="l a{max(starts)}"><rect class="cursor" x="{x0}" y="{y - 13}" width="8" height="15" fill="{TEXT}"/></g>'
    return starts, style, f'<g clip-path="url(#{clip}c)"><g class="{clip}">' + "".join(body) + cursor + "</g></g>"


l_starts, l_style, l_body = render(left, 20, "s0")
r_starts, r_style, r_body = render(right, PANE + 21, "s1")
appear = "\n".join(
    f"@keyframes a{t} {{ 0% {{ opacity: 0 }} {t}% {{ opacity: 1 }} 97% {{ opacity: 1 }} 100% {{ opacity: 0 }} }} .a{t} {{ animation-name: a{t}; }}"
    for t in sorted(l_starts | r_starts))

print(f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" role="img" aria-label="Animated split terminal: outpost setup prepares a new server, gateway included, on the left; on the right, plain outpost opens the menu and gives a project an HTTPS address">
<style>
text {{ font-family: ui-monospace, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace; font-size: 13px; white-space: pre; }}
.b {{ font-weight: 700; }}
g.l {{ opacity: 0; animation-duration: {LOOP}s; animation-iteration-count: infinite; animation-timing-function: steps(1, end); }}
@keyframes cursor {{ 0%, 49% {{ opacity: 1 }} 50%, 100% {{ opacity: 0 }} }}
.cursor {{ animation: cursor 1s infinite; }}
{appear}
{l_style}
{r_style}
@media (prefers-reduced-motion: reduce) {{ g.l {{ animation: none; opacity: 1; }} .s0, .s1 {{ animation: none; }} }}
</style>
<defs><clipPath id="s0c"><rect x="0" y="{TOP}" width="{PANE}" height="{H - TOP}"/></clipPath><clipPath id="s1c"><rect x="{PANE + 1}" y="{TOP}" width="{PANE}" height="{H - TOP}"/></clipPath></defs>
<rect width="{W}" height="{H}" rx="14" fill="{BG}"/>
<rect x="0.5" y="0.5" width="{W - 1}" height="{H - 1}" rx="14" fill="none" stroke="{RULE}"/>
<circle cx="24" cy="22" r="6" fill="#FF5F57"/><circle cx="44" cy="22" r="6" fill="#FEBC2E"/><circle cx="64" cy="22" r="6" fill="#28C840"/>
<line x1="{PANE + 0.5}" y1="40" x2="{PANE + 0.5}" y2="{H}" stroke="{RULE}"/>
<line x1="0" y1="40.5" x2="{W}" y2="40.5" stroke="{RULE}"/>
<text x="{PANE / 2}" y="27" fill="{MUTED}" text-anchor="middle" style="font-size:12px">a new server</text>
<text x="{PANE + PANE / 2}" y="27" fill="{MUTED}" text-anchor="middle" style="font-size:12px">the next day</text>
{l_body}
{r_body}
</svg>''')
