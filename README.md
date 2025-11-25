# quick-npm-module-scanner

Just a very simple (dependency-less) scanner which quickly scans node_modules folders against a list of possible IOCs with module names.

The ioc.txt file is a list of possible IOCs with module names, as seen in several blog posts like the current one at:

- https://www.heise.de/en/news/Shai-Hulud-2-New-version-of-NPM-worm-also-attacks-low-code-platforms-11089785.html
- https://www.koi.ai/incident/live-updates-sha1-hulud-the-second-coming-hundred-npm-packages-compromised
- https://www.aikido.dev/blog/shai-hulud-strikes-again-hitting-zapier-ensdomains
