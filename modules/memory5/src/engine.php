<?php
// Memory 5 engine, shared by src/cli.php and src/api.php: config, redaction,
// the SQLite store (FTS5 plus optional vectors), indexing, hybrid search and
// the data frame around every result. Needs PHP 8.1+ with pdo_sqlite (FTS5);
// no other extension is used.
declare(strict_types=1);

namespace Memory5;

const SCHEMA = 1;
const REDACTED = '[redacted]';
const SOURCES = ['file', 'journal', 'memory'];
const BEGIN_MARK = 'memory5: begin retrieved data';
const END_MARK = 'memory5: end retrieved data';

/** A failure with an exit code: 1 runtime, 2 usage or bad request. */
final class Fail extends \RuntimeException
{
}

function fail(string $msg, int $code = 1): never
{
    throw new Fail($msg, $code);
}

function strict_errors(): void
{
    date_default_timezone_set('UTC');
    set_error_handler(static function (int $no, string $msg, string $file, int $line): bool {
        if ((error_reporting() & $no) === 0) {
            return false; // silenced with @: the caller checks the return value
        }
        throw new \ErrorException($msg, 0, $no, $file, $line);
    });
}

// ---------------------------------------------------------------- config

/** Parses the TOML subset config.default.toml uses: tables, strings, numbers, booleans, one-line arrays. */
function toml_parse(string $text, string $file): array
{
    $out = [];
    $path = [];
    foreach (preg_split('/\R/', $text) as $i => $raw) {
        $line = trim($raw);
        $at = $file . ':' . ($i + 1);
        if ($line === '' || $line[0] === '#') {
            continue;
        }
        if (preg_match('/^\[([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*)\]\s*(?:#.*)?$/', $line, $m)) {
            $path = explode('.', $m[1]);
            continue;
        }
        if (!preg_match('/^([A-Za-z0-9_-]+)\s*=\s*(.*)$/', $line, $m)) {
            fail("$at: not a TOML key = value line");
        }
        $pos = 0;
        $value = toml_value($m[2], $pos, $at);
        $rest = trim(substr($m[2], $pos));
        if ($rest !== '' && $rest[0] !== '#') {
            fail("$at: unexpected text after the value");
        }
        $ref = &$out;
        foreach ($path as $p) {
            $ref[$p] ??= [];
            $ref = &$ref[$p];
        }
        $ref[$m[1]] = $value;
        unset($ref);
    }
    return $out;
}

function toml_value(string $s, int &$pos, string $at): mixed
{
    $pos += strspn($s, " \t", $pos);
    $c = $s[$pos] ?? '';
    if ($c === '"') {
        $pos++;
        $v = '';
        while ($pos < strlen($s) && $s[$pos] !== '"') {
            if ($s[$pos] === '\\') {
                $e = $s[$pos + 1] ?? '';
                $map = ['"' => '"', '\\' => '\\', 'n' => "\n", 't' => "\t", 'r' => "\r"];
                if (isset($map[$e])) {
                    $v .= $map[$e];
                    $pos += 2;
                    continue;
                }
                if ($e === 'u' && preg_match('/^[0-9A-Fa-f]{4}/', substr($s, $pos + 2, 4))) {
                    $v .= mb_chr_utf8(hexdec(substr($s, $pos + 2, 4)));
                    $pos += 6;
                    continue;
                }
                fail("$at: unsupported escape in a string");
            }
            $v .= $s[$pos++];
        }
        if (($s[$pos] ?? '') !== '"') {
            fail("$at: unterminated string");
        }
        $pos++;
        return $v;
    }
    if ($c === "'") {
        $end = strpos($s, "'", $pos + 1);
        if ($end === false) {
            fail("$at: unterminated string");
        }
        $v = substr($s, $pos + 1, $end - $pos - 1);
        $pos = $end + 1;
        return $v;
    }
    if ($c === '[') {
        $pos++;
        $list = [];
        while (true) {
            $pos += strspn($s, " \t", $pos);
            if (($s[$pos] ?? '') === ']') {
                $pos++;
                return $list;
            }
            $list[] = toml_value($s, $pos, $at);
            $pos += strspn($s, " \t", $pos);
            $d = $s[$pos] ?? '';
            if ($d === ',') {
                $pos++;
            } elseif ($d !== ']') {
                fail("$at: arrays must stay on one line and separate items with commas");
            }
        }
    }
    if (preg_match('/^(true|false)\b/', substr($s, $pos), $m)) {
        $pos += strlen($m[1]);
        return $m[1] === 'true';
    }
    if (preg_match('/^[+-]?\d[\d_]*(\.\d[\d_]*)?([eE][+-]?\d+)?/', substr($s, $pos), $m)) {
        $pos += strlen($m[0]);
        $n = str_replace('_', '', $m[0]);
        return ($m[1] ?? '') !== '' || ($m[2] ?? '') !== '' ? (float) $n : (int) $n;
    }
    fail("$at: unsupported value");
}

function mb_chr_utf8(int $cp): string
{
    return html_entity_decode('&#' . $cp . ';', ENT_QUOTES | ENT_HTML5, 'UTF-8');
}

/** Tables merge, lists and scalars replace (the core's rule). */
function config_merge(array $base, array $over): array
{
    foreach ($over as $k => $v) {
        if (is_array($v) && isset($base[$k]) && is_array($base[$k]) && !array_is_list($base[$k]) && !array_is_list($v)) {
            $base[$k] = config_merge($base[$k], $v);
        } else {
            $base[$k] = $v;
        }
    }
    return $base;
}

/** Defaults from config.default.toml, overlaid by KOMBINAT_CONFIG (the core passes the merged config). */
function load_config(): array
{
    $file = dirname(__DIR__) . '/config.default.toml';
    $text = @file_get_contents($file);
    if ($text === false) {
        fail('cannot read config.default.toml');
    }
    $cfg = toml_parse($text, 'config.default.toml');
    $json = getenv('KOMBINAT_CONFIG');
    if ($json !== false && trim($json) !== '') {
        $over = json_decode($json, true);
        if (!is_array($over)) {
            fail('KOMBINAT_CONFIG is not a JSON object', 2);
        }
        $cfg = config_merge($cfg, $over);
    }
    return $cfg;
}

final class Ctx
{
    public array $cfg;
    public string $data;
    private ?\PDO $pdo = null;
    /** @var list<array{0:string,1:string|callable}> */
    private array $rules = [];

    public function __construct()
    {
        $this->cfg = load_config();
        $data = getenv('KOMBINAT_DATA');
        if ($data === false || $data === '') {
            fail('KOMBINAT_DATA is not set; the core sets it, standalone runs set it by hand to an absolute directory', 2);
        }
        if ($data[0] !== '/') {
            fail('KOMBINAT_DATA must be an absolute path', 2);
        }
        umask(0077);
        if (!is_dir($data) && !@mkdir($data, 0700, true)) {
            fail('cannot create KOMBINAT_DATA');
        }
        $this->data = rtrim($data, '/');
        $this->rules = redaction_rules($this);
    }

    public function get(string $key, string $type): mixed
    {
        $v = $this->cfg;
        foreach (explode('.', $key) as $p) {
            if (!is_array($v) || !array_key_exists($p, $v)) {
                fail("config: missing key $key");
            }
            $v = $v[$p];
        }
        $ok = match ($type) {
            'int' => is_int($v),
            'float' => is_float($v) || is_int($v),
            'bool' => is_bool($v),
            'string' => is_string($v),
            'list' => is_array($v) && array_is_list($v) && count(array_filter($v, 'is_string')) === count($v),
        };
        if (!$ok) {
            fail("config: $key must be of type $type", 2);
        }
        return $type === 'float' ? (float) $v : $v;
    }

    public function int(string $key, int $min = 0): int
    {
        $v = $this->get($key, 'int');
        if ($v < $min) {
            fail("config: $key must be at least $min", 2);
        }
        return $v;
    }

    public function db(): \PDO
    {
        return $this->pdo ??= open_db($this->data . '/memory5.db');
    }

    /** @return array{0:string,1:int} redacted text and the number of redactions */
    public function redact(string $text): array
    {
        $n = 0;
        foreach ($this->rules as [$re, $with]) {
            $count = 0;
            $text = is_callable($with)
                ? preg_replace_callback($re, $with, $text, -1, $count)
                : preg_replace($re, $with, $text, -1, $count);
            if ($text === null) {
                fail('redaction failed: ' . preg_last_error_msg());
            }
            $n += $count;
        }
        return [$text, $n];
    }

    public function text(string $key, array $vars = []): string
    {
        static $cache = [];
        $loc = $this->get('output.locale', 'string');
        if (!in_array($loc, ['en', 'pl'], true)) {
            fail('config: output.locale must be "en" or "pl"', 2);
        }
        if (!isset($cache[$loc])) {
            $raw = @file_get_contents(dirname(__DIR__) . "/locales/$loc.json");
            $cache[$loc] = is_string($raw) ? (json_decode($raw, true) ?: []) : [];
        }
        $s = $cache[$loc]["module.memory5.$key"] ?? $key;
        foreach ($vars as $k => $v) {
            $s = str_replace('{' . $k . '}', (string) $v, $s);
        }
        return $s;
    }
}

// ---------------------------------------------------------------- redaction

/** Built-in secret shapes, then optional key = value pairs, then the operator's patterns. */
function redaction_rules(Ctx $x): array
{
    $keep1 = static fn (array $m): string => $m[1] . REDACTED;
    $rules = [
        ['/-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)/s', REDACTED],
        ['/\b(?:gh[pousr]_|github_pat_)[A-Za-z0-9_]{20,}/', REDACTED],
        ['/\bglpat-[A-Za-z0-9_-]{20,}/', REDACTED],
        ['/\bxox[abeoprs]-[A-Za-z0-9-]{10,}/', REDACTED],
        ['/\bsk-[A-Za-z0-9_-]{20,}/', REDACTED],
        ['/\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}/', REDACTED],
        ['/\bhf_[A-Za-z0-9]{20,}/', REDACTED],
        ['/\bnpm_[A-Za-z0-9]{30,}/', REDACTED],
        ['/\b(?:AKIA|ASIA)[0-9A-Z]{16}\b/', REDACTED],
        ['/\bAIza[0-9A-Za-z_-]{35}/', REDACTED],
        ['/\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}/', REDACTED],
        ['/\b((?:Bearer|Basic|Token)\s+)[A-Za-z0-9._~+\/=-]{16,}/i', $keep1],
        ['~(\b[a-z][a-z0-9+.-]*://[^\s:/@]+:)[^\s@/]+(?=@)~i', $keep1],
    ];
    if ($x->get('redact.assignments', 'bool')) {
        $rules[] = [
            '/(\b[A-Za-z0-9_.-]*(?:passw(?:or)?d|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential|authorization)[A-Za-z0-9_.-]*["\']?[ \t]*[:=][ \t]*)(?!["\']?\[redacted\])("[^"\n]*"|\'[^\'\n]*\'|[^\s,;]+)/i',
            static fn (array $m): string => $m[1] . REDACTED,
        ];
    }
    foreach ($x->get('redact.patterns', 'list') as $i => $p) {
        $re = '~' . str_replace('~', '\~', $p) . '~u';
        if (@preg_match($re, '') === false) {
            fail("config: redact.patterns[$i] is not a valid pattern", 2);
        }
        $rules[] = [$re, REDACTED];
    }
    return $rules;
}

// ---------------------------------------------------------------- store

function open_db(string $file): \PDO
{
    umask(0077);
    $pdo = new \PDO('sqlite:' . $file, null, null, [\PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION]);
    $pdo->exec('PRAGMA busy_timeout = 5000');
    $pdo->exec('PRAGMA journal_mode = WAL');
    $pdo->exec('PRAGMA foreign_keys = ON');
    $pdo->exec('PRAGMA secure_delete = ON');
    $v = (int) $pdo->query('PRAGMA user_version')->fetchColumn();
    if ($v > SCHEMA) {
        fail("memory5.db has schema $v, newer than this module ($v > " . SCHEMA . '); refusing to touch it');
    }
    if ($v < 1) {
        $pdo->beginTransaction();
        try {
            $pdo->exec(<<<'SQL'
                CREATE TABLE memories (id INTEGER PRIMARY KEY, text TEXT NOT NULL, created_at TEXT NOT NULL);
                CREATE TABLE journal (id INTEGER PRIMARY KEY, day TEXT NOT NULL, text TEXT NOT NULL, created_at TEXT NOT NULL);
                CREATE INDEX journal_day ON journal(day);
                CREATE TABLE docs (id INTEGER PRIMARY KEY, source TEXT NOT NULL, ref TEXT NOT NULL, title TEXT NOT NULL,
                    version TEXT NOT NULL, hash TEXT NOT NULL, indexed_at TEXT NOT NULL, UNIQUE (source, ref));
                CREATE TABLE chunks (id INTEGER PRIMARY KEY, doc_id INTEGER NOT NULL REFERENCES docs(id) ON DELETE CASCADE,
                    seq INTEGER NOT NULL, heading TEXT NOT NULL, body TEXT NOT NULL, hash TEXT NOT NULL);
                CREATE INDEX chunks_doc ON chunks(doc_id);
                CREATE INDEX chunks_hash ON chunks(hash);
                CREATE TABLE vectors (hash TEXT NOT NULL, model TEXT NOT NULL, dim INTEGER NOT NULL, vec BLOB NOT NULL,
                    PRIMARY KEY (hash, model));
                CREATE TABLE state (key TEXT PRIMARY KEY, value TEXT NOT NULL);
                SQL);
            try {
                $pdo->exec("CREATE VIRTUAL TABLE chunks_fts USING fts5(norm, tokenize = 'unicode61 remove_diacritics 2')");
            } catch (\PDOException) {
                fail('this PHP build has SQLite without FTS5; memory5 needs FTS5');
            }
            $pdo->exec('PRAGMA user_version = 1');
            $pdo->commit();
        } catch (\Throwable $e) {
            $pdo->rollBack();
            throw $e;
        }
    }
    return $pdo;
}

function now_utc(): string
{
    return gmdate('Y-m-d\TH:i:s\Z');
}

function state_set(\PDO $db, string $key, string $value): void
{
    $db->prepare('INSERT INTO state (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value')
        ->execute([$key, $value]);
}

function state_get(\PDO $db, string $key): string
{
    $s = $db->prepare('SELECT value FROM state WHERE key = ?');
    $s->execute([$key]);
    $v = $s->fetchColumn();
    return $v === false ? '' : (string) $v;
}

/** Folds the letters unicode61 cannot strip diacritics from, so "l" also finds "ł". */
function norm(string $s): string
{
    return strtr($s, [
        "\u{142}" => 'l', "\u{141}" => 'L', "\u{111}" => 'd', "\u{110}" => 'D',
        "\u{F8}" => 'o', "\u{D8}" => 'O', "\u{127}" => 'h', "\u{126}" => 'H',
    ]);
}

function ulen(string $s): int
{
    return (int) preg_match_all('/./us', $s);
}

function uhead(string $s, int $chars): string
{
    preg_match('/^.{0,' . max(0, $chars) . '}/us', $s, $m);
    return $m[0] ?? '';
}

/** The longest prefix of $s that is at most $bytes long and ends on a character boundary. */
function bytes_head(string $s, int $bytes): string
{
    if (strlen($s) <= $bytes) {
        return $s;
    }
    $cut = substr($s, 0, $bytes);
    while ($cut !== '' && preg_match('//u', $cut) !== 1) {
        $cut = substr($cut, 0, -1);
    }
    return $cut;
}

function is_text(string $s): bool
{
    return !str_contains($s, "\0") && preg_match('//u', $s) === 1;
}

/**
 * Splits text into chunks of about $max bytes; a Markdown heading starts a new chunk.
 * @return list<array{0:string,1:string}> heading and body
 */
function chunk_text(string $text, int $max): array
{
    $max = max(64, $max);
    $out = [];
    $cur = [];
    $size = 0;
    $heading = '';
    $flush = static function () use (&$out, &$cur, &$size, &$heading): void {
        $body = trim(implode("\n", $cur));
        if ($body !== '') {
            $out[] = [$heading, $body];
        }
        $cur = [];
        $size = 0;
    };
    foreach (preg_split('/\R/u', $text) as $line) {
        if (preg_match('/^#{1,6}[ \t]+(.+?)[ \t#]*$/u', $line, $m)) {
            $flush();
            $heading = bytes_head($m[1], 200);
        } elseif ($cur !== [] && $size + strlen($line) + 1 > $max) {
            $flush();
        }
        while (strlen($line) > $max) {
            $head = bytes_head($line, $max);
            $cur[] = $head;
            $flush();
            $line = substr($line, strlen($head));
        }
        $cur[] = $line;
        $size += strlen($line) + 1;
    }
    $flush();
    return $out;
}

function context_line(string $title, string $heading): string
{
    return $heading === '' ? $title : "$title \u{203A} $heading";
}

function drop_doc(\PDO $db, string $source, string $ref): bool
{
    $s = $db->prepare('SELECT id FROM docs WHERE source = ? AND ref = ?');
    $s->execute([$source, $ref]);
    $id = $s->fetchColumn();
    if ($id === false) {
        return false;
    }
    $db->prepare('DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE doc_id = ?)')->execute([$id]);
    $db->prepare('DELETE FROM docs WHERE id = ?')->execute([$id]);
    return true;
}

/** Replaces one document's chunks; $text is already redacted. Returns the chunk count. */
function put_doc(Ctx $x, string $source, string $ref, string $title, string $version, string $text): int
{
    $db = $x->db();
    $chunks = chunk_text($text, $x->int('index.chunk_bytes', 64));
    $db->beginTransaction();
    try {
        drop_doc($db, $source, $ref);
        $db->prepare('INSERT INTO docs (source, ref, title, version, hash, indexed_at) VALUES (?, ?, ?, ?, ?, ?)')
            ->execute([$source, $ref, $title, $version, sha1($text), now_utc()]);
        $doc = (int) $db->lastInsertId();
        $ins = $db->prepare('INSERT INTO chunks (doc_id, seq, heading, body, hash) VALUES (?, ?, ?, ?, ?)');
        $fts = $db->prepare('INSERT INTO chunks_fts (rowid, norm) VALUES (?, ?)');
        foreach ($chunks as $i => [$heading, $body]) {
            $full = context_line($title, $heading) . "\n" . $body;
            $ins->execute([$doc, $i, $heading, $body, sha1($full)]);
            $fts->execute([(int) $db->lastInsertId(), norm($full)]);
        }
        $db->commit();
    } catch (\Throwable $e) {
        $db->rollBack();
        throw $e;
    }
    return count($chunks);
}

// ---------------------------------------------------------------- events

/** Appends one event for the core; standalone (KOMBINAT_EVENTS unset) nothing is logged. Never pass indexed text. */
function emit(string $verb, string $subject, string $summary, array $payload): void
{
    $file = getenv('KOMBINAT_EVENTS');
    if ($file === false || $file === '') {
        return;
    }
    $line = json_encode(
        ['action' => "memory5.$verb", 'subject' => $subject, 'summary' => $summary, 'payload' => (object) $payload],
        JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR
    );
    file_put_contents($file, $line . "\n", FILE_APPEND | LOCK_EX);
}

// ---------------------------------------------------------------- files

/** Why a path may never be indexed, whatever the config says; null when it may. */
function hard_exclusion(string $path): ?string
{
    foreach (explode('/', trim($path, '/')) as $part) {
        if (strcasecmp($part, 'secrets') === 0) {
            return 'inside a secrets directory';
        }
        if ($part !== '' && $part[0] === '.') {
            return 'hidden path';
        }
    }
    $name = basename($path);
    if (preg_match('/(secret|token|passw(?:or)?d|credential|private[_-]?key)/i', $name)) {
        return 'secret-shaped file name';
    }
    if (preg_match('/^id_(?:rsa|dsa|ecdsa|ed25519)|\.(?:pem|key|p12|pfx|jks|kdbx|gpg|asc|der|crt)$/i', $name)) {
        return 'key material';
    }
    if (preg_match('/\.(?:db|sqlite|sqlite3)(?:-wal|-shm|-journal)?$/i', $name)) {
        return 'database file';
    }
    return null;
}

/** Lexically normalises an absolute path (no symlinks resolved). */
function clean_path(string $path): string
{
    $parts = [];
    foreach (explode('/', $path) as $p) {
        if ($p === '' || $p === '.') {
            continue;
        }
        if ($p === '..') {
            array_pop($parts);
            continue;
        }
        $parts[] = $p;
    }
    return '/' . implode('/', $parts);
}

/**
 * Resolves index.files into the files to index.
 * @return array{files: array<string,string>, excluded: list<array{0:string,1:string}>, missing: list<string>, capped: int}
 *         files maps real path → title
 */
function collect_files(Ctx $x): array
{
    $base = $x->get('index.base', 'string');
    if ($base !== '' && $base[0] !== '/') {
        fail('config: index.base must be an absolute path', 2);
    }
    $exts = array_map('strtolower', $x->get('index.extensions', 'list'));
    $skipDirs = $x->get('index.exclude_dirs', 'list');
    $maxBytes = $x->int('index.max_file_kb', 1) * 1024;
    $maxFiles = $x->int('index.max_files', 1);
    $r = ['files' => [], 'excluded' => [], 'missing' => [], 'capped' => 0];

    $consider = static function (string $path) use (&$r, $exts, $maxBytes, $maxFiles, $base): void {
        $ext = strtolower(pathinfo($path, PATHINFO_EXTENSION));
        if (!in_array($ext, $exts, true)) {
            return;
        }
        $real = realpath($path);
        if ($real === false || !is_file($real)) {
            $r['missing'][] = $path;
            return;
        }
        $why = hard_exclusion($path) ?? hard_exclusion($real);
        if ($why !== null) {
            $r['excluded'][] = [$path, $why];
            return;
        }
        if (isset($r['files'][$real])) {
            return;
        }
        if (filesize($real) > $maxBytes) {
            $r['excluded'][] = [$path, 'larger than index.max_file_kb'];
            return;
        }
        if (count($r['files']) >= $maxFiles) {
            $r['capped']++;
            return;
        }
        $title = $base !== '' && str_starts_with($real, rtrim($base, '/') . '/') ? substr($real, strlen(rtrim($base, '/')) + 1) : $real;
        $r['files'][$real] = $title;
    };

    foreach ($x->get('index.files', 'list') as $entry) {
        if ($entry === '') {
            continue;
        }
        if ($entry[0] !== '/') {
            if ($base === '') {
                $r['excluded'][] = [$entry, 'relative path while index.base is empty'];
                continue;
            }
            $entry = $base . '/' . $entry;
        }
        $path = clean_path($entry);
        $why = hard_exclusion($path);
        if ($why === null && file_exists($path)) {
            $real = realpath($path);
            $why = $real === false ? null : hard_exclusion($real);
        }
        if ($why !== null) {
            $r['excluded'][] = [$path, $why];
            continue;
        }
        if (!file_exists($path)) {
            $r['missing'][] = $path;
            continue;
        }
        if (!is_dir($path)) {
            $consider($path);
            continue;
        }
        $dir = new \RecursiveDirectoryIterator(realpath($path), \FilesystemIterator::SKIP_DOTS | \FilesystemIterator::CURRENT_AS_FILEINFO);
        $filter = new \RecursiveCallbackFilterIterator($dir, static function (\SplFileInfo $f) use (&$r, $skipDirs): bool {
            if (!$f->isDir()) {
                return true;
            }
            $name = $f->getFilename();
            $why = match (true) {
                $f->isLink() => 'symlinked directory',
                strcasecmp($name, 'secrets') === 0 => 'inside a secrets directory',
                $name[0] === '.' => null, // hidden directories are skipped silently (.git and friends)
                in_array($name, $skipDirs, true) => 'listed in index.exclude_dirs',
                default => false,
            };
            if (is_string($why)) {
                $r['excluded'][] = [$f->getPathname() . '/', $why];
            }
            return $why === false;
        });
        $found = [];
        foreach (new \RecursiveIteratorIterator($filter) as $f) {
            if ($f->getFilename()[0] !== '.') {
                $found[] = $f->getPathname();
            }
        }
        sort($found, SORT_STRING);
        foreach ($found as $p) {
            $consider($p);
        }
    }
    ksort($r['files'], SORT_STRING);
    return $r;
}

// ---------------------------------------------------------------- embeddings

/**
 * Embeds texts through embed.command or embed.url; null with $err set on any failure.
 * @return list<list<float>>|null unit vectors, one per text
 */
function embed(Ctx $x, array $texts, bool $query, ?string &$err): ?array
{
    $err = null;
    if (!$x->get('embed.enabled', 'bool')) {
        $err = 'embeddings are off (embed.enabled = false)';
        return null;
    }
    if ($texts === []) {
        return [];
    }
    $req = json_encode(['model' => $x->get('embed.model', 'string'), 'input' => array_values($texts)],
        JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
    $timeout = $query ? $x->int('embed.query_timeout_seconds', 1) : $x->int('embed.timeout_seconds', 1);
    $cmd = $x->get('embed.command', 'list');
    $url = $x->get('embed.url', 'string');
    if ($cmd !== []) {
        $out = run_embedder($cmd, $req, $timeout, $err);
    } elseif ($url !== '') {
        $out = post_embedder($url, $req, $timeout, $err);
    } else {
        $err = 'embed.enabled is true but neither embed.command nor embed.url is set';
        return null;
    }
    if ($out === null) {
        return null;
    }
    $res = json_decode($out, true);
    $vecs = is_array($res) ? ($res['embeddings'] ?? null) : null;
    if (!is_array($vecs) || count($vecs) !== count($texts)) {
        $err = 'the embedder did not answer {"embeddings": [...]} with one vector per input';
        return null;
    }
    $dim = null;
    $outv = [];
    foreach ($vecs as $v) {
        if (!is_array($v) || $v === [] || count(array_filter($v, static fn ($n) => is_int($n) || is_float($n))) !== count($v)) {
            $err = 'the embedder returned a vector that is not a list of numbers';
            return null;
        }
        $dim ??= count($v);
        if (count($v) !== $dim) {
            $err = 'the embedder returned vectors of different lengths';
            return null;
        }
        $len = sqrt(array_sum(array_map(static fn ($n) => $n * $n, $v)));
        $outv[] = array_map(static fn ($n) => $len > 0 ? $n / $len : 0.0, array_values($v));
    }
    return $outv;
}

function run_embedder(array $argv, string $input, int $timeout, ?string &$err): ?string
{
    $env = [];
    foreach (['PATH', 'HOME', 'LANG', 'LC_ALL', 'TMPDIR', 'TZ'] as $k) {
        $v = getenv($k);
        if ($v !== false) {
            $env[$k] = $v;
        }
    }
    $env['PATH'] ??= '/usr/local/bin:/usr/bin:/bin';
    try {
        $p = proc_open($argv, [0 => ['pipe', 'r'], 1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes, null, $env);
    } catch (\Throwable) {
        $p = false;
    }
    if (!is_resource($p)) {
        $err = 'embed.command could not be started';
        return null;
    }
    foreach ($pipes as $pipe) {
        stream_set_blocking($pipe, false);
    }
    $deadline = microtime(true) + $timeout;
    $out = '';
    $off = 0;
    $open = [1 => $pipes[1], 2 => $pipes[2]];
    $in = $pipes[0];
    while ($open !== [] && microtime(true) < $deadline) {
        $r = array_values($open);
        $w = $in !== null ? [$in] : [];
        $e = null;
        if (@stream_select($r, $w, $e, 0, 100000) === false) {
            break;
        }
        if ($w !== [] && $in !== null) {
            $n = @fwrite($in, substr($input, $off, 65536));
            $off += $n === false ? 0 : $n;
            if ($n === false || $off >= strlen($input)) {
                fclose($in);
                $in = null;
            }
        }
        foreach ($r as $s) {
            $chunk = fread($s, 65536);
            if ($s === ($open[1] ?? null)) {
                $out .= (string) $chunk;
                if (strlen($out) > 64 << 20) {
                    $deadline = 0;
                }
            }
            if (feof($s)) {
                fclose($s);
                $open = array_filter($open, static fn ($o) => $o !== $s);
            }
        }
    }
    if ($in !== null) {
        fclose($in);
    }
    if ($open !== []) {
        proc_terminate($p, 9);
        foreach ($open as $s) {
            fclose($s);
        }
        proc_close($p);
        $err = "embed.command did not finish within $timeout s";
        return null;
    }
    $code = proc_close($p);
    if ($code !== 0) {
        $err = "embed.command exited with code $code";
        return null;
    }
    return $out;
}

function post_embedder(string $url, string $body, int $timeout, ?string &$err): ?string
{
    $u = parse_url($url);
    $host = strtolower(trim((string) ($u['host'] ?? ''), '[]'));
    if (!in_array($u['scheme'] ?? '', ['http', 'https'], true) || !in_array($host, ['127.0.0.1', '::1', 'localhost'], true)) {
        $err = 'embed.url must be an http(s) URL on 127.0.0.1, ::1 or localhost';
        return null;
    }
    $ctx = stream_context_create(['http' => [
        'method' => 'POST',
        'header' => "Content-Type: application/json\r\n",
        'content' => $body,
        'timeout' => $timeout,
        'ignore_errors' => true,
        'follow_location' => 0,
    ]]);
    try {
        $out = file_get_contents($url, false, $ctx);
    } catch (\Throwable) {
        $out = false;
    }
    $headers = function_exists('http_get_last_response_headers') ? (http_get_last_response_headers() ?? []) : ($http_response_header ?? []);
    if ($out === false || !preg_match('#^HTTP/\S+\s+200\b#', (string) ($headers[0] ?? ''))) {
        $err = 'embed.url did not answer 200';
        return null;
    }
    return $out;
}

function pack_vec(array $v): string
{
    return pack('g*', ...$v);
}

function unpack_vec(string $blob): array
{
    return array_values(unpack('g*', $blob));
}

function dot(array $a, array $b): float
{
    $s = 0.0;
    $n = min(count($a), count($b));
    for ($i = 0; $i < $n; $i++) {
        $s += $a[$i] * $b[$i];
    }
    return $s;
}

// ---------------------------------------------------------------- operations

/** One index run: files, journal days and memories, then missing vectors, within index.budget_seconds. */
function op_index(Ctx $x, bool $rebuild): array
{
    $lock = fopen($x->data . '/index.lock', 'c');
    if ($lock === false || !flock($lock, LOCK_EX | LOCK_NB)) {
        return ['status' => 'busy', 'message' => 'another index run holds the lock; try again later'];
    }
    $start = microtime(true);
    $budget = $x->int('index.budget_seconds', 1);
    $over = static fn (): bool => microtime(true) - $start > $budget;
    $db = $x->db();
    $rep = ['status' => 'complete', 'rebuild' => $rebuild, 'files_seen' => 0, 'files_indexed' => 0, 'unchanged' => 0,
        'removed' => 0, 'excluded' => 0, 'missing' => 0, 'capped' => 0, 'failed' => 0, 'chunks' => 0, 'redactions' => 0,
        'journal_days' => 0, 'memories' => 0, 'vectors_added' => 0, 'vectors_pending' => 0, 'vector_error' => '',
        'excluded_paths' => [], 'missing_paths' => []];
    if ($rebuild) {
        $db->exec('DELETE FROM chunks_fts; DELETE FROM chunks; DELETE FROM docs;');
    }

    $c = collect_files($x);
    $rep['files_seen'] = count($c['files']);
    $rep['excluded'] = count($c['excluded']);
    $rep['missing'] = count($c['missing']);
    $rep['capped'] = $c['capped'];
    $rep['excluded_paths'] = $c['excluded'];
    $rep['missing_paths'] = $c['missing'];

    $get = $db->prepare("SELECT version, hash FROM docs WHERE source = 'file' AND ref = ?");
    $touch = $db->prepare("UPDATE docs SET version = ? WHERE source = 'file' AND ref = ?");
    foreach ($c['files'] as $real => $title) {
        if ($over()) {
            $rep['status'] = 'partial';
            break;
        }
        $st = @stat($real);
        if ($st === false) {
            $rep['failed']++;
            continue;
        }
        $version = $st['mtime'] . ':' . $st['size'];
        $get->execute([$real]);
        $doc = $get->fetch(\PDO::FETCH_ASSOC) ?: null;
        $get->closeCursor();
        if ($doc !== null && $doc['version'] === $version) {
            $rep['unchanged']++;
            continue;
        }
        $text = @file_get_contents($real);
        if ($text === false || !is_text($text)) {
            $rep['failed']++;
            $rep['excluded_paths'][] = [$real, $text === false ? 'unreadable' : 'not UTF-8 text'];
            if ($doc !== null && drop_doc($db, 'file', $real)) {
                $rep['removed']++;
            }
            continue;
        }
        [$text, $n] = $x->redact($text);
        $rep['redactions'] += $n;
        if ($doc !== null && $doc['hash'] === sha1($text)) {
            $touch->execute([$version, $real]);
            $rep['unchanged']++;
            continue;
        }
        $rep['chunks'] += put_doc($x, 'file', $real, $title, $version, $text);
        $rep['files_indexed']++;
    }
    // Files no longer listed, now excluded or gone leave the index.
    $known = $db->query("SELECT ref FROM docs WHERE source = 'file'")->fetchAll(\PDO::FETCH_COLUMN);
    foreach ($known as $ref) {
        if (!isset($c['files'][$ref]) && drop_doc($db, 'file', $ref)) {
            $rep['removed']++;
        }
    }

    // Journal days and memories are already redacted at write time; this only repairs the derived index.
    $days = $db->query('SELECT day, COUNT(*) || \':\' || MAX(id) AS v FROM journal GROUP BY day')->fetchAll(\PDO::FETCH_KEY_PAIR);
    $have = $db->query("SELECT ref, version FROM docs WHERE source = 'journal'")->fetchAll(\PDO::FETCH_KEY_PAIR);
    foreach ($days as $day => $v) {
        if (($have[$day] ?? null) !== $v) {
            $rep['chunks'] += index_journal_day($x, (string) $day);
            $rep['journal_days']++;
        }
    }
    $mems = $db->query('SELECT id, text FROM memories')->fetchAll(\PDO::FETCH_KEY_PAIR);
    $have = $db->query("SELECT ref, version FROM docs WHERE source = 'memory'")->fetchAll(\PDO::FETCH_KEY_PAIR);
    foreach ($mems as $id => $text) {
        if (!isset($have[(string) $id])) {
            $rep['chunks'] += put_doc($x, 'memory', (string) $id, "Memory #$id", (string) $id, (string) $text);
            $rep['memories']++;
        }
    }
    foreach (array_keys($have) as $ref) {
        if (!isset($mems[(int) $ref])) {
            drop_doc($db, 'memory', (string) $ref);
        }
    }

    // Vectors for chunks that have none for the configured model.
    if ($x->get('embed.enabled', 'bool')) {
        $model = $x->get('embed.model', 'string');
        $batch = $x->int('embed.batch', 1);
        $pending = $db->prepare('SELECT DISTINCT c.hash, d.title, c.heading, c.body FROM chunks c JOIN docs d ON d.id = c.doc_id
            WHERE NOT EXISTS (SELECT 1 FROM vectors v WHERE v.hash = c.hash AND v.model = ?) LIMIT ?');
        $put = $db->prepare('INSERT OR REPLACE INTO vectors (hash, model, dim, vec) VALUES (?, ?, ?, ?)');
        while (!$over()) {
            $pending->execute([$model, $batch]);
            $rows = $pending->fetchAll(\PDO::FETCH_ASSOC);
            if ($rows === []) {
                break;
            }
            $texts = array_map(static fn ($r) => context_line($r['title'], $r['heading']) . "\n" . $r['body'], $rows);
            $vecs = embed($x, $texts, false, $err);
            if ($vecs === null) {
                $rep['vector_error'] = (string) $err;
                break;
            }
            $db->beginTransaction();
            foreach ($rows as $i => $r) {
                $put->execute([$r['hash'], $model, count($vecs[$i]), pack_vec($vecs[$i])]);
            }
            $db->commit();
            $rep['vectors_added'] += count($rows);
        }
        $s = $db->prepare('SELECT COUNT(DISTINCT c.hash) FROM chunks c WHERE NOT EXISTS
            (SELECT 1 FROM vectors v WHERE v.hash = c.hash AND v.model = ?)');
        $s->execute([$model]);
        $rep['vectors_pending'] = (int) $s->fetchColumn();
        if ($rep['vectors_pending'] > 0 && $rep['status'] === 'complete' && $rep['vector_error'] === '') {
            $rep['status'] = 'partial';
        }
    }
    $db->exec('DELETE FROM vectors WHERE hash NOT IN (SELECT hash FROM chunks)');

    $rep['seconds'] = round(microtime(true) - $start, 3);
    state_set($db, 'last_index_at', now_utc());
    state_set($db, 'last_index_status', $rep['status']);
    state_set($db, 'last_vector_error', $rep['vector_error']);
    $payload = array_diff_key($rep, ['excluded_paths' => 1, 'missing_paths' => 1, 'seconds' => 1]);
    $payload['vector_error'] = $rep['vector_error'] !== '';
    emit('indexed', 'index', sprintf('%s index run: %d indexed, %d unchanged, %d removed, %d excluded',
        $rep['status'], $rep['files_indexed'], $rep['unchanged'], $rep['removed'], $rep['excluded']), $payload);
    flock($lock, LOCK_UN);
    fclose($lock);
    return $rep;
}

function journal_tz(Ctx $x): \DateTimeZone
{
    $tz = $x->get('journal.timezone', 'string');
    if ($tz === '') {
        $tz = (string) getenv('TZ');
    }
    try {
        return new \DateTimeZone($tz !== '' ? ltrim($tz, ':') : 'UTC');
    } catch (\Throwable) {
        if ($x->get('journal.timezone', 'string') !== '') {
            fail('config: journal.timezone is not a known time zone', 2);
        }
        return new \DateTimeZone('UTC');
    }
}

function index_journal_day(Ctx $x, string $day): int
{
    $db = $x->db();
    $s = $db->prepare('SELECT id, text, created_at FROM journal WHERE day = ? ORDER BY id');
    $s->execute([$day]);
    $rows = $s->fetchAll(\PDO::FETCH_ASSOC);
    if ($rows === []) {
        drop_doc($db, 'journal', $day);
        return 0;
    }
    $tz = journal_tz($x);
    $parts = [];
    foreach ($rows as $r) {
        $t = (new \DateTimeImmutable($r['created_at']))->setTimezone($tz)->format('H:i');
        $parts[] = "## $t (#{$r['id']})\n{$r['text']}";
    }
    $last = end($rows);
    return put_doc($x, 'journal', $day, "Journal $day", count($rows) . ':' . $last['id'], implode("\n\n", $parts));
}

function check_text(string $text, int $max, string $what): void
{
    if (trim($text) === '') {
        fail("$what is empty", 2);
    }
    if (strlen($text) > $max) {
        fail("$what is longer than $max bytes", 2);
    }
    if (!is_text($text)) {
        fail("$what is not UTF-8 text", 2);
    }
}

function op_memory_add(Ctx $x, string $text): array
{
    check_text($text, $x->int('memory.max_bytes', 1), 'the memory');
    [$text, $n] = $x->redact(trim($text));
    $db = $x->db();
    $db->prepare('INSERT INTO memories (text, created_at) VALUES (?, ?)')->execute([$text, now_utc()]);
    $id = (int) $db->lastInsertId();
    put_doc($x, 'memory', (string) $id, "Memory #$id", (string) $id, $text);
    emit('memory.added', "memory/$id", "added memory #$id", ['id' => $id, 'bytes' => strlen($text), 'redactions' => $n]);
    return ['id' => $id, 'bytes' => strlen($text), 'redactions' => $n];
}

function op_memory_delete(Ctx $x, int $id): array
{
    $db = $x->db();
    $db->beginTransaction();
    $s = $db->prepare('DELETE FROM memories WHERE id = ?');
    $s->execute([$id]);
    $gone = $s->rowCount() > 0;
    drop_doc($db, 'memory', (string) $id);
    $db->commit();
    if (!$gone) {
        fail("no memory #$id");
    }
    emit('memory.deleted', "memory/$id", "deleted memory #$id", ['id' => $id]);
    return ['id' => $id, 'deleted' => true];
}

function op_memory_list(Ctx $x, int $limit): array
{
    $s = $x->db()->prepare('SELECT id, created_at, text FROM memories ORDER BY id DESC LIMIT ?');
    $s->execute([$limit]);
    return ['memories' => array_map(static fn ($r) => ['id' => (int) $r['id'], 'created_at' => $r['created_at'],
        'text' => $r['text']], $s->fetchAll(\PDO::FETCH_ASSOC))];
}

function op_journal_add(Ctx $x, string $text): array
{
    check_text($text, $x->int('journal.max_bytes', 1), 'the journal entry');
    [$text, $n] = $x->redact(trim($text));
    $day = (new \DateTimeImmutable('now', journal_tz($x)))->format('Y-m-d');
    $db = $x->db();
    $db->prepare('INSERT INTO journal (day, text, created_at) VALUES (?, ?, ?)')->execute([$day, $text, now_utc()]);
    $id = (int) $db->lastInsertId();
    index_journal_day($x, $day);
    emit('journal.added', "journal/$day", "added journal entry #$id", ['id' => $id, 'day' => $day,
        'bytes' => strlen($text), 'redactions' => $n]);
    return ['id' => $id, 'day' => $day, 'bytes' => strlen($text), 'redactions' => $n];
}

function op_journal_list(Ctx $x, string $day, int $limit): array
{
    if ($day !== '' && !preg_match('/^\d{4}-\d{2}-\d{2}$/', $day)) {
        fail('--day wants YYYY-MM-DD', 2);
    }
    $sql = 'SELECT id, day, created_at, text FROM journal' . ($day !== '' ? ' WHERE day = :day' : '') . ' ORDER BY id DESC LIMIT :n';
    $s = $x->db()->prepare($sql);
    if ($day !== '') {
        $s->bindValue(':day', $day);
    }
    $s->bindValue(':n', $limit, \PDO::PARAM_INT);
    $s->execute();
    return ['entries' => array_map(static fn ($r) => ['id' => (int) $r['id'], 'day' => $r['day'],
        'created_at' => $r['created_at'], 'text' => $r['text']], $s->fetchAll(\PDO::FETCH_ASSOC))];
}

/** Builds an FTS5 MATCH expression from letters and digits only, each term quoted, so input cannot inject syntax. */
function fts_query(Ctx $x, string $q): string
{
    $min = $x->int('search.min_term_chars', 1);
    $prefixMin = $x->int('search.prefix_min_chars', 1);
    $trim = $x->int('search.prefix_trim_chars');
    $stop = array_flip(array_map('strtolower', $x->get('search.stopwords', 'list')));
    preg_match_all('/[\p{L}\p{N}]+/u', norm($q), $m);
    $terms = [];
    foreach ($m[0] as $t) {
        $len = ulen($t);
        $num = preg_match('/^\p{N}+$/u', $t) === 1;
        if (!$num && ($len < $min || isset($stop[strtolower($t)]))) {
            continue;
        }
        if (!$num && $len >= $prefixMin && $len - $trim >= $min) {
            $terms['"' . uhead($t, $len - $trim) . '"*'] = true;
        } else {
            $terms['"' . $t . '"'] = true;
        }
    }
    return implode(' OR ', array_keys($terms));
}

function op_query(Ctx $x, string $q, ?int $limit, string $source): array
{
    $q = trim($q);
    if ($q === '' || !is_text($q)) {
        fail('the query is empty or not UTF-8 text', 2);
    }
    if (strlen($q) > 4096) {
        fail('the query is longer than 4096 bytes', 2);
    }
    if ($source !== '' && !in_array($source, SOURCES, true)) {
        fail('source must be one of: ' . implode(', ', SOURCES), 2);
    }
    $limit ??= $x->int('search.limit', 1);
    $limit = max(1, min($limit, $x->int('search.max_limit', 1)));
    $cand = max($limit, $x->int('search.candidates', 1));
    $k = $x->int('search.rrf_k', 1);
    $db = $x->db();
    $where = $source !== '' ? ' AND d.source = :src' : '';

    $fts = [];
    $match = fts_query($x, $q);
    if ($match !== '') {
        $s = $db->prepare("SELECT c.id FROM chunks_fts JOIN chunks c ON c.id = chunks_fts.rowid JOIN docs d ON d.id = c.doc_id
            WHERE chunks_fts MATCH :m$where ORDER BY bm25(chunks_fts) LIMIT :n");
        $s->bindValue(':m', $match);
        if ($source !== '') {
            $s->bindValue(':src', $source);
        }
        $s->bindValue(':n', $cand, \PDO::PARAM_INT);
        $s->execute();
        $fts = array_map('intval', $s->fetchAll(\PDO::FETCH_COLUMN));
    }

    $vec = [];
    $vecErr = '';
    $used = false;
    if ($x->get('embed.enabled', 'bool')) {
        $qv = embed($x, [$x->get('embed.query_prefix', 'string') . $q], true, $err);
        if ($qv === null) {
            $vecErr = (string) $err;
        } else {
            $used = true;
            $s = $db->prepare("SELECT c.id, v.vec FROM chunks c JOIN docs d ON d.id = c.doc_id
                JOIN vectors v ON v.hash = c.hash AND v.model = :model WHERE 1 = 1$where");
            $s->bindValue(':model', $x->get('embed.model', 'string'));
            if ($source !== '') {
                $s->bindValue(':src', $source);
            }
            $s->execute();
            while ($r = $s->fetch(\PDO::FETCH_NUM)) {
                $vec[(int) $r[0]] = dot($qv[0], unpack_vec($r[1]));
            }
            arsort($vec);
            $vec = array_slice($vec, 0, $cand, true);
        }
    }

    $minSim = (float) $x->get('search.min_similarity', 'float');
    $score = [];
    $how = [];
    foreach ($fts as $i => $id) {
        $score[$id] = 1 / ($k + $i + 1);
        $how[$id] = 'fts';
    }
    $rank = 0;
    foreach ($vec as $id => $sim) {
        $rank++;
        if (!isset($score[$id]) && $sim < $minSim) {
            continue;
        }
        $score[$id] = ($score[$id] ?? 0) + 1 / ($k + $rank);
        $how[$id] = isset($how[$id]) ? 'both' : 'vector';
    }
    arsort($score);
    $score = array_slice($score, 0, $limit, true);

    $results = [];
    if ($score !== []) {
        $ids = implode(',', array_map('intval', array_keys($score)));
        $rows = $db->query("SELECT c.id, c.heading, c.body, d.source, d.ref, d.title FROM chunks c JOIN docs d ON d.id = c.doc_id
            WHERE c.id IN ($ids)")->fetchAll(\PDO::FETCH_ASSOC);
        $byId = array_column($rows, null, 'id');
        foreach ($score as $id => $sc) {
            $r = $byId[$id] ?? null;
            if ($r === null) {
                continue;
            }
            $results[] = ['rank' => count($results) + 1, 'source' => $r['source'], 'ref' => $r['ref'], 'title' => $r['title'],
                'heading' => $r['heading'], 'match' => $how[$id], 'score' => round($sc, 6),
                'similarity' => isset($vec[$id]) ? round($vec[$id], 4) : null, 'text' => $r['body']];
        }
    }
    $counts = array_count_values(array_column($results, 'match'));
    emit('queried', $source !== '' ? "search/$source" : 'search', sprintf('query answered with %d results', count($results)), [
        'results' => count($results), 'fts' => $counts['fts'] ?? 0, 'vector' => $counts['vector'] ?? 0, 'both' => $counts['both'] ?? 0,
        'vectors_used' => $used, 'vector_error' => $vecErr !== '', 'source' => $source]);
    $out = ['query' => $q, 'notice' => $x->text('frame_notice', ['count' => count($results)]), 'results' => $results,
        'vectors' => ['used' => $used, 'error' => $vecErr]];
    $out['framed'] = frame($out);
    return $out;
}

/** Strips control and bidi-override characters, so retrieved text cannot move the cursor or reorder the frame. */
function clean(string $s, bool $oneLine): string
{
    $s = preg_replace('/[\x{202A}-\x{202E}\x{2066}-\x{2069}\x{200E}\x{200F}]/u', '', $s) ?? '';
    $s = preg_replace($oneLine ? '/[\x00-\x1F\x7F]/' : '/[\x00-\x08\x0B-\x1F\x7F]/', $oneLine ? ' ' : '', $s) ?? '';
    return $s;
}

/**
 * The data frame: a notice, a begin marker, every result as a header plus its text with each line quoted
 * by "> ", and an end marker. Retrieved text can never produce an unquoted line inside the frame.
 */
function frame(array $q): string
{
    $lines = ['memory5: ' . $q['notice'], BEGIN_MARK];
    foreach ($q['results'] as $r) {
        $where = clean(context_line($r['title'], $r['heading']), true);
        $ref = clean($r['ref'], true);
        $sim = $r['similarity'] !== null ? sprintf(', similarity %.4f', $r['similarity']) : '';
        $lines[] = sprintf('[%d] source: %s %s | %s (%s, score %.6f%s)', $r['rank'], $r['source'], $ref, $where, $r['match'], $r['score'], $sim);
        foreach (preg_split('/\R/u', clean($r['text'], false)) as $l) {
            $lines[] = '> ' . $l;
        }
    }
    $lines[] = END_MARK;
    return implode("\n", $lines) . "\n";
}

function op_status(Ctx $x): array
{
    $db = $x->db();
    $docs = array_fill_keys(SOURCES, 0);
    foreach ($db->query('SELECT source, COUNT(*) FROM docs GROUP BY source')->fetchAll(\PDO::FETCH_KEY_PAIR) as $s => $n) {
        $docs[$s] = (int) $n;
    }
    $model = $x->get('embed.model', 'string');
    $s = $db->prepare('SELECT COUNT(*) FROM vectors WHERE model = ?');
    $s->execute([$model]);
    $stored = (int) $s->fetchColumn();
    $s = $db->prepare('SELECT COUNT(DISTINCT c.hash) FROM chunks c WHERE NOT EXISTS (SELECT 1 FROM vectors v WHERE v.hash = c.hash AND v.model = ?)');
    $s->execute([$model]);
    $size = 0;
    foreach (['', '-wal', '-shm'] as $suf) {
        $f = $x->data . '/memory5.db' . $suf;
        $size += is_file($f) ? (int) filesize($f) : 0;
    }
    return [
        'docs' => $docs,
        'chunks' => (int) $db->query('SELECT COUNT(*) FROM chunks')->fetchColumn(),
        'memories' => (int) $db->query('SELECT COUNT(*) FROM memories')->fetchColumn(),
        'journal_entries' => (int) $db->query('SELECT COUNT(*) FROM journal')->fetchColumn(),
        'vectors' => ['enabled' => $x->get('embed.enabled', 'bool'), 'model' => $model, 'stored' => $stored,
            'pending' => $x->get('embed.enabled', 'bool') ? (int) $s->fetchColumn() : 0,
            'last_error' => state_get($db, 'last_vector_error')],
        'last_index_at' => state_get($db, 'last_index_at'),
        'last_index_status' => state_get($db, 'last_index_status'),
        'db_bytes' => $size,
    ];
}

function json_out(mixed $v): string
{
    return json_encode($v, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE | JSON_PRETTY_PRINT | JSON_THROW_ON_ERROR) . "\n";
}
