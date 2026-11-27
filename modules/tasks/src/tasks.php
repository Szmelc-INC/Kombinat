<?php
// Tasks: the one handler behind both entrypoints. src/cli.php turns argv into
// a request and src/api.php reads one as JSON from stdin; both call respond().
// State lives in KOMBINAT_DATA/tasks.sqlite. Every change commits first, then
// appends one event line to KOMBINAT_EVENTS (unset when run standalone: no events).
declare(strict_types=1);

namespace Kombinat\Tasks;

use PDO;
use stdClass;

// One type, seven states, in workflow order; any state may follow any other.
const STATES = ['idea', 'planned', 'queued', 'running', 'done', 'failed', 'dropped'];
// The operations; provides.api names them as tasks.<op>.
const OPS = ['add', 'list', 'show', 'update', 'delete', 'comment.add', 'comment.edit', 'comment.delete'];
const DB_FILE = 'tasks.sqlite';
const SCHEMA_VERSION = 1;
// An owner or comment author: an unverified label, lowercase like an actor name.
const NAME_RE = '/^[a-z0-9][a-z0-9.-]{0,63}$/';
const PRIORITY_MAX = 1000000;
// The core reads event lines of at most 64 KiB and summaries of at most 1000 bytes.
const EVENT_LINE_MAX = 60000;
const SUMMARY_MAX = 900;
const SUMMARY_TITLE_MAX = 120;
const JSON_FLAGS = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR;
// Exit code per failure kind, shared by the CLI and the api entrypoint.
const EXIT_CODES = ['internal' => 1, 'invalid' => 2, 'not_found' => 3, 'conflict' => 4, 'config' => 5];

// The defaults mirror config.default.toml; tests/tasks.sh checks that they match.
const DEFAULTS = [
    'enabled' => false,
    'tasks' => ['default_status' => 'idea', 'title_max_bytes' => 200, 'body_max_bytes' => 16384],
    'comments' => ['default_author' => 'operator', 'text_max_bytes' => 8192, 'max_depth' => 32],
    'list' => ['default_limit' => 200, 'max_limit' => 1000],
    'storage' => ['busy_timeout_ms' => 5000],
];

const SCHEMA_1 = <<<'SQL'
CREATE TABLE tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('idea', 'planned', 'queued', 'running', 'done', 'failed', 'dropped')),
  priority INTEGER NOT NULL DEFAULT 0,
  owner TEXT NOT NULL DEFAULT '',
  parent_id INTEGER REFERENCES tasks(id),
  body TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX tasks_status ON tasks(status);
CREATE INDEX tasks_parent ON tasks(parent_id);
CREATE TABLE comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id INTEGER NOT NULL REFERENCES tasks(id),
  reply_to INTEGER REFERENCES comments(id),
  author TEXT NOT NULL,
  text TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX comments_task ON comments(task_id);
SQL;

final class Failure extends \RuntimeException
{
    public function __construct(public readonly string $kind, string $message)
    {
        parent::__construct($message);
    }
}

function fail(string $kind, string $message): never
{
    throw new Failure($kind, $message);
}

// strict turns every PHP warning into an exception, so nothing but the
// answer reaches stdout.
function strict(): void
{
    error_reporting(E_ALL);
    ini_set('display_errors', 'stderr');
    set_error_handler(static function (int $no, string $msg, string $file, int $line): bool {
        if (!(error_reporting() & $no)) {
            return false;
        }
        throw new \ErrorException($msg, 0, $no, $file, $line);
    });
}

// respond runs one request and returns [response document, exit code].
function respond(array $req): array
{
    $op = $req['op'] ?? null;
    try {
        if (!is_string($op) || !in_array($op, OPS, true)) {
            fail('invalid', 'op must be one of ' . implode(', ', OPS));
        }
        unset($req['op']);
        $cfg = config();
        $db = open($cfg);
        [$data, $event] = dispatch($db, $cfg, $op, $req);
        if ($event !== null) {
            emit($event);
        }
        return [['ok' => true, 'op' => $op, 'data' => $data], 0];
    } catch (Failure $e) {
        $code = $e->kind;
    } catch (\Throwable $e) {
        $code = 'internal';
    }
    $doc = ['ok' => false, 'op' => is_string($op) ? $op : null, 'error' => ['code' => $code, 'message' => $e->getMessage()]];
    return [$doc, EXIT_CODES[$code]];
}

function dispatch(PDO $db, array $cfg, string $op, array $req): array
{
    return match ($op) {
        'add' => write($db, fn () => op_add($db, $cfg, $req)),
        'list' => [op_list($db, $cfg, $req), null],
        'show' => [op_show($db, $req), null],
        'update' => write($db, fn () => op_update($db, $cfg, $req)),
        'delete' => write($db, fn () => op_delete($db, $req)),
        'comment.add' => write($db, fn () => op_comment_add($db, $cfg, $req)),
        'comment.edit' => write($db, fn () => op_comment_edit($db, $cfg, $req)),
        'comment.delete' => write($db, fn () => op_comment_delete($db, $req)),
    };
}

// ---- config ----------------------------------------------------------------

// config merges KOMBINAT_CONFIG (the core's merged module config) over the
// defaults and validates every key the code reads.
function config(): array
{
    $cfg = DEFAULTS;
    $raw = getenv('KOMBINAT_CONFIG');
    if (is_string($raw) && $raw !== '') {
        try {
            $in = json_decode($raw, true, 32, JSON_THROW_ON_ERROR);
        } catch (\JsonException $e) {
            fail('config', 'KOMBINAT_CONFIG is not JSON: ' . $e->getMessage());
        }
        if (!is_array($in)) {
            fail('config', 'KOMBINAT_CONFIG must be a JSON object');
        }
        $cfg = array_replace_recursive($cfg, $in);
    }
    foreach (['tasks', 'comments', 'list', 'storage'] as $table) {
        if (!is_array($cfg[$table] ?? null)) {
            fail('config', "$table must be a table");
        }
    }
    $int = static function (string $table, string $key, int $min, int $max) use ($cfg): void {
        $v = $cfg[$table][$key] ?? null;
        if (!is_int($v) || $v < $min || $v > $max) {
            fail('config', "$table.$key must be an integer from $min to $max");
        }
    };
    $int('tasks', 'title_max_bytes', 1, 1000);
    $int('tasks', 'body_max_bytes', 2, 262144);
    $int('comments', 'text_max_bytes', 1, 262144);
    $int('comments', 'max_depth', 1, 100);
    $int('list', 'max_limit', 1, 10000);
    $int('list', 'default_limit', 1, $cfg['list']['max_limit']);
    $int('storage', 'busy_timeout_ms', 0, 60000);
    if (!in_array($cfg['tasks']['default_status'] ?? null, STATES, true)) {
        fail('config', 'tasks.default_status must be one of ' . implode(', ', STATES));
    }
    $author = $cfg['comments']['default_author'] ?? null;
    if (!is_string($author) || !preg_match(NAME_RE, $author)) {
        fail('config', 'comments.default_author must be a lowercase name (letters, digits, dot, hyphen)');
    }
    return $cfg;
}

// ---- storage ---------------------------------------------------------------

function open(array $cfg): PDO
{
    $dir = getenv('KOMBINAT_DATA');
    if (!is_string($dir) || $dir === '') {
        fail('config', 'KOMBINAT_DATA is not set: run through kombinat, or point it at a private directory to run standalone');
    }
    if (!is_dir($dir)) {
        fail('config', 'KOMBINAT_DATA is not a directory: ' . $dir);
    }
    $db = new PDO('sqlite:' . $dir . '/' . DB_FILE, null, null, [
        PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
        PDO::ATTR_DEFAULT_FETCH_MODE => PDO::FETCH_ASSOC,
    ]);
    $db->exec('PRAGMA busy_timeout = ' . $cfg['storage']['busy_timeout_ms']);
    $db->exec('PRAGMA foreign_keys = ON');
    $db->exec('PRAGMA journal_mode = WAL');
    migrate($db);
    return $db;
}

// migrate moves the schema forward only; a newer schema than the code is refused.
function migrate(PDO $db): void
{
    $version = static fn (): int => (int) $db->query('PRAGMA user_version')->fetchColumn();
    if ($version() === SCHEMA_VERSION) {
        return;
    }
    write($db, static function () use ($db, $version): array {
        $v = $version();
        if ($v > SCHEMA_VERSION) {
            fail('config', "the database schema is version $v, newer than this module's " . SCHEMA_VERSION);
        }
        if ($v < 1) {
            $db->exec(SCHEMA_1);
        }
        $db->exec('PRAGMA user_version = ' . SCHEMA_VERSION);
        return [null, null];
    });
}

// write runs fn in one immediate transaction: all of it commits, or none.
function write(PDO $db, callable $fn): array
{
    $db->exec('BEGIN IMMEDIATE');
    try {
        $result = $fn();
        $db->exec('COMMIT');
        return $result;
    } catch (\Throwable $e) {
        try {
            $db->exec('ROLLBACK');
        } catch (\Throwable) {
            // The transaction is gone already; the first error is the one to report.
        }
        throw $e;
    }
}

function now(): string
{
    return gmdate('Y-m-d\TH:i:s\Z');
}

function one(PDO $db, string $sql, array $params): ?array
{
    $st = $db->prepare($sql);
    $st->execute($params);
    $row = $st->fetch();
    return $row === false ? null : $row;
}

function all(PDO $db, string $sql, array $params): array
{
    $st = $db->prepare($sql);
    $st->execute($params);
    return $st->fetchAll();
}

function find_task(PDO $db, int $id): array
{
    return one($db, 'SELECT * FROM tasks WHERE id = ?', [$id]) ?? fail('not_found', "no task #$id");
}

function find_comment(PDO $db, int $id): array
{
    return one($db, 'SELECT * FROM comments WHERE id = ?', [$id]) ?? fail('not_found', "no comment #$id");
}

function task_out(array $r): array
{
    return [
        'id' => (int) $r['id'],
        'title' => $r['title'],
        'status' => $r['status'],
        'priority' => (int) $r['priority'],
        'owner' => $r['owner'],
        'parent' => $r['parent_id'] === null ? null : (int) $r['parent_id'],
        'body' => json_decode($r['body'], false, 64, JSON_THROW_ON_ERROR),
        'created_at' => $r['created_at'],
        'updated_at' => $r['updated_at'],
    ];
}

function comment_out(array $r): array
{
    return [
        'id' => (int) $r['id'],
        'task' => (int) $r['task_id'],
        'reply_to' => $r['reply_to'] === null ? null : (int) $r['reply_to'],
        'author' => $r['author'],
        'text' => $r['text'],
        'created_at' => $r['created_at'],
        'updated_at' => $r['updated_at'],
    ];
}

// ---- validation ------------------------------------------------------------

function only(array $req, array $allowed): void
{
    $unknown = array_diff(array_keys($req), $allowed);
    if ($unknown) {
        fail('invalid', 'unknown fields: ' . implode(', ', $unknown) . ' (allowed: ' . implode(', ', $allowed) . ')');
    }
}

function id_field(mixed $v, string $name): int
{
    if (!is_int($v) || $v < 1) {
        fail('invalid', "$name must be a positive integer id");
    }
    return $v;
}

function text_field(mixed $v, string $name, int $max, bool $multiline): string
{
    if (!is_string($v)) {
        fail('invalid', "$name must be text");
    }
    if (!preg_match('//u', $v)) {
        fail('invalid', "$name must be valid UTF-8");
    }
    $v = trim($v);
    if ($v === '') {
        fail('invalid', "$name must not be empty");
    }
    if (strlen($v) > $max) {
        fail('invalid', "$name is longer than $max bytes");
    }
    $control = $multiline ? '/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/' : '/[\x00-\x1f\x7f]/';
    if (preg_match($control, $v)) {
        fail('invalid', "$name holds control characters");
    }
    return $v;
}

function status_field(mixed $v): string
{
    if (!is_string($v) || !in_array($v, STATES, true)) {
        fail('invalid', 'status must be one of ' . implode(', ', STATES));
    }
    return $v;
}

function name_field(mixed $v, string $name, bool $empty_ok): string
{
    if (!is_string($v) || !(($empty_ok && $v === '') || preg_match(NAME_RE, $v))) {
        fail('invalid', "$name must be a lowercase name (letters, digits, dot, hyphen)" . ($empty_ok ? ' or empty' : ''));
    }
    return $v;
}

// task_fields validates the settable task fields into column values.
function task_fields(PDO $db, array $cfg, array $req): array
{
    $f = [];
    if (array_key_exists('title', $req)) {
        $f['title'] = text_field($req['title'], 'title', $cfg['tasks']['title_max_bytes'], false);
    }
    if (array_key_exists('status', $req)) {
        $f['status'] = status_field($req['status']);
    }
    if (array_key_exists('priority', $req)) {
        $p = $req['priority'];
        if (!is_int($p) || abs($p) > PRIORITY_MAX) {
            fail('invalid', 'priority must be an integer from -' . PRIORITY_MAX . ' to ' . PRIORITY_MAX);
        }
        $f['priority'] = $p;
    }
    if (array_key_exists('owner', $req)) {
        $f['owner'] = name_field($req['owner'], 'owner', true);
    }
    if (array_key_exists('parent', $req)) {
        $p = $req['parent'];
        if ($p !== null) {
            $p = id_field($p, 'parent');
            find_task($db, $p);
        }
        $f['parent_id'] = $p;
    }
    if (array_key_exists('body', $req)) {
        if (!$req['body'] instanceof stdClass) {
            fail('invalid', 'body must be a JSON object');
        }
        $body = json_encode($req['body'], JSON_FLAGS);
        $max = $cfg['tasks']['body_max_bytes'];
        if (strlen($body) > $max) {
            fail('invalid', "body is longer than $max bytes as JSON");
        }
        $f['body'] = $body;
    }
    return $f;
}

// ---- task operations -------------------------------------------------------

function op_add(PDO $db, array $cfg, array $req): array
{
    only($req, ['title', 'status', 'priority', 'owner', 'parent', 'body']);
    if (!array_key_exists('title', $req)) {
        fail('invalid', 'title is required');
    }
    $f = task_fields($db, $cfg, $req) + [
        'status' => $cfg['tasks']['default_status'],
        'priority' => 0,
        'owner' => '',
        'parent_id' => null,
        'body' => '{}',
    ];
    $f['created_at'] = $f['updated_at'] = now();
    $cols = array_keys($f);
    $db->prepare('INSERT INTO tasks (' . implode(', ', $cols) . ') VALUES (:' . implode(', :', $cols) . ')')->execute($f);
    $task = task_out(find_task($db, (int) $db->lastInsertId()));
    $lean = $task;
    $lean['body'] = ['omitted' => true];
    return [['task' => $task], [
        'action' => 'add',
        'subject' => 'task/' . $task['id'],
        'summary' => "Added task #{$task['id']} ({$task['status']}): " . cut($task['title'], SUMMARY_TITLE_MAX),
        'payload' => ['task' => $task],
        'lean' => [['task' => $lean]],
    ]];
}

function op_list(PDO $db, array $cfg, array $req): array
{
    only($req, ['status', 'owner', 'parent', 'limit']);
    $where = [];
    $params = [];
    if (array_key_exists('status', $req)) {
        $where[] = 't.status = :status';
        $params['status'] = status_field($req['status']);
    }
    if (array_key_exists('owner', $req)) {
        $where[] = 't.owner = :owner';
        $params['owner'] = name_field($req['owner'], 'owner', true);
    }
    if (array_key_exists('parent', $req)) {
        if ($req['parent'] === null) {
            $where[] = 't.parent_id IS NULL';
        } else {
            $where[] = 't.parent_id = :parent';
            $params['parent'] = id_field($req['parent'], 'parent');
        }
    }
    $limit = $req['limit'] ?? $cfg['list']['default_limit'];
    $max = $cfg['list']['max_limit'];
    if (!is_int($limit) || $limit < 1 || $limit > $max) {
        fail('invalid', "limit must be an integer from 1 to $max");
    }
    $rows = all($db, 'SELECT t.id, t.title, t.status, t.priority, t.owner, t.parent_id, t.updated_at,'
        . ' (SELECT COUNT(*) FROM comments c WHERE c.task_id = t.id) AS comments FROM tasks t'
        . ($where ? ' WHERE ' . implode(' AND ', $where) : '')
        . ' ORDER BY t.priority DESC, t.id DESC LIMIT ' . $limit, $params);
    $tasks = array_map(static fn (array $r): array => [
        'id' => (int) $r['id'],
        'title' => $r['title'],
        'status' => $r['status'],
        'priority' => (int) $r['priority'],
        'owner' => $r['owner'],
        'parent' => $r['parent_id'] === null ? null : (int) $r['parent_id'],
        'comments' => (int) $r['comments'],
        'updated_at' => $r['updated_at'],
    ], $rows);
    return ['tasks' => $tasks];
}

function op_show(PDO $db, array $req): array
{
    only($req, ['id']);
    $task = task_out(find_task($db, id_field($req['id'] ?? null, 'id')));
    $children = array_map(static fn (array $r): array => [
        'id' => (int) $r['id'],
        'title' => $r['title'],
        'status' => $r['status'],
    ], all($db, 'SELECT id, title, status FROM tasks WHERE parent_id = ? ORDER BY priority DESC, id', [$task['id']]));
    $comments = array_map('Kombinat\Tasks\comment_out', all($db, 'SELECT * FROM comments WHERE task_id = ? ORDER BY id', [$task['id']]));
    return ['task' => $task, 'children' => $children, 'comments' => thread($comments, null)];
}

// thread nests comments under the comment they reply to, oldest first.
function thread(array $comments, ?int $parent): array
{
    $out = [];
    foreach ($comments as $c) {
        if ($c['reply_to'] === $parent) {
            $c['replies'] = thread($comments, $c['id']);
            $out[] = $c;
        }
    }
    return $out;
}

function op_update(PDO $db, array $cfg, array $req): array
{
    only($req, ['id', 'title', 'status', 'priority', 'owner', 'parent', 'body']);
    $id = id_field($req['id'] ?? null, 'id');
    unset($req['id']);
    if (!$req) {
        fail('invalid', 'update needs at least one of title, status, priority, owner, parent, body');
    }
    $before = find_task($db, $id);
    $f = task_fields($db, $cfg, $req);
    if (array_key_exists('parent_id', $f) && $f['parent_id'] !== null) {
        // The new parent must not be this task or one of its descendants.
        for ($p = $f['parent_id']; $p !== null;) {
            if ($p === $id) {
                fail('conflict', "task #$id cannot sit under task #{$f['parent_id']}: that would make a cycle");
            }
            $up = find_task($db, $p)['parent_id'];
            $p = $up === null ? null : (int) $up;
        }
    }
    $changes = [];
    foreach ($f as $col => $to) {
        $from = $col === 'priority' || ($col === 'parent_id' && $before[$col] !== null) ? (int) $before[$col] : $before[$col];
        if ($from === $to) {
            continue;
        }
        $key = $col === 'parent_id' ? 'parent' : $col;
        $changes[$key] = $col === 'body'
            ? ['from' => json_decode($from, false, 64, JSON_THROW_ON_ERROR), 'to' => json_decode($to, false, 64, JSON_THROW_ON_ERROR)]
            : ['from' => $from, 'to' => $to];
    }
    if (!$changes) {
        return [['task' => task_out($before), 'changes' => new stdClass()], null];
    }
    $f['updated_at'] = now();
    $sets = implode(', ', array_map(static fn (string $c): string => "$c = :$c", array_keys($f)));
    $db->prepare("UPDATE tasks SET $sets WHERE id = :id")->execute($f + ['id' => $id]);
    $task = task_out(find_task($db, $id));
    $lean = $changes;
    if (isset($lean['body'])) {
        $lean['body'] = ['omitted' => true];
    }
    return [['task' => $task, 'changes' => $changes], [
        'action' => 'update',
        'subject' => "task/$id",
        'summary' => "Updated task #$id: " . describe($changes),
        'payload' => ['id' => $id, 'changes' => $changes],
        'lean' => [['id' => $id, 'changes' => $lean]],
    ]];
}

// describe names each changed field with its new value; title and body are
// only named, since the payload carries both sides in full.
function describe(array $changes): string
{
    $named = array_map(static fn (string $key, array $c): string => match ($key) {
        'title', 'body' => $key,
        'owner' => 'owner=' . ($c['to'] === '' ? 'none' : $c['to']),
        'parent' => 'parent=' . ($c['to'] === null ? 'none' : '#' . $c['to']),
        default => "$key=" . $c['to'],
    }, array_keys($changes), array_values($changes));
    return implode(', ', $named);
}

function op_delete(PDO $db, array $req): array
{
    only($req, ['id']);
    $id = id_field($req['id'] ?? null, 'id');
    $task = task_out(find_task($db, $id));
    $kids = (int) one($db, 'SELECT COUNT(*) AS n FROM tasks WHERE parent_id = ?', [$id])['n'];
    if ($kids > 0) {
        fail('conflict', "task #$id has $kids child tasks: delete them or move them to another parent first");
    }
    $comments = array_map('Kombinat\Tasks\comment_out', all($db, 'SELECT * FROM comments WHERE task_id = ? ORDER BY id', [$id]));
    $db->prepare('DELETE FROM comments WHERE task_id = ?')->execute([$id]);
    $db->prepare('DELETE FROM tasks WHERE id = ?')->execute([$id]);
    // The event carries the removed rows, so the log alone can rebuild them.
    $bare = $task;
    $bare['body'] = ['omitted' => true];
    return [['deleted' => $task, 'comments' => count($comments)], [
        'action' => 'delete',
        'subject' => "task/$id",
        'summary' => "Deleted task #$id: " . cut($task['title'], SUMMARY_TITLE_MAX),
        'payload' => ['deleted' => $task, 'comments' => $comments],
        'lean' => [
            ['deleted' => $task, 'comments' => [], 'comments_omitted' => count($comments)],
            ['deleted' => $bare, 'comments' => [], 'comments_omitted' => count($comments)],
        ],
    ]];
}

// ---- comment operations ----------------------------------------------------

function op_comment_add(PDO $db, array $cfg, array $req): array
{
    only($req, ['task', 'text', 'reply_to', 'author']);
    $taskId = id_field($req['task'] ?? null, 'task');
    $text = text_field($req['text'] ?? null, 'text', $cfg['comments']['text_max_bytes'], true);
    $author = array_key_exists('author', $req) ? name_field($req['author'], 'author', false) : $cfg['comments']['default_author'];
    find_task($db, $taskId);
    $replyTo = $req['reply_to'] ?? null;
    if ($replyTo !== null) {
        $replyTo = id_field($replyTo, 'reply_to');
        $parent = find_comment($db, $replyTo);
        if ((int) $parent['task_id'] !== $taskId) {
            fail('conflict', "comment #$replyTo belongs to task #{$parent['task_id']}, not to task #$taskId");
        }
        // A top-level comment is level 1; each reply is one level below the comment it answers.
        $level = 2;
        for ($up = $parent['reply_to']; $up !== null; $up = find_comment($db, (int) $up)['reply_to']) {
            $level++;
        }
        $max = $cfg['comments']['max_depth'];
        if ($level > $max) {
            fail('conflict', "a reply to comment #$replyTo would sit at level $level; threads go at most $max levels deep");
        }
    }
    $now = now();
    $db->prepare('INSERT INTO comments (task_id, reply_to, author, text, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)')
        ->execute([$taskId, $replyTo, $author, $text, $now, $now]);
    $comment = comment_out(find_comment($db, (int) $db->lastInsertId()));
    $lean = $comment;
    $lean['text'] = ['omitted' => true];
    $what = $replyTo === null ? '' : " in reply to #$replyTo";
    return [['comment' => $comment], [
        'action' => 'comment.add',
        'subject' => "task/$taskId",
        'summary' => "Comment #{$comment['id']} on task #$taskId by $author$what",
        'payload' => ['task' => $taskId, 'comment' => $comment],
        'lean' => [['task' => $taskId, 'comment' => $lean]],
    ]];
}

function op_comment_edit(PDO $db, array $cfg, array $req): array
{
    only($req, ['id', 'text']);
    $id = id_field($req['id'] ?? null, 'id');
    $text = text_field($req['text'] ?? null, 'text', $cfg['comments']['text_max_bytes'], true);
    $before = find_comment($db, $id);
    if ($before['text'] === $text) {
        return [['comment' => comment_out($before), 'changed' => false], null];
    }
    $db->prepare('UPDATE comments SET text = ?, updated_at = ? WHERE id = ?')->execute([$text, now(), $id]);
    $comment = comment_out(find_comment($db, $id));
    $taskId = $comment['task'];
    return [['comment' => $comment, 'changed' => true], [
        'action' => 'comment.edit',
        'subject' => "task/$taskId",
        'summary' => "Edited comment #$id on task #$taskId",
        'payload' => ['task' => $taskId, 'comment' => $id, 'from' => $before['text'], 'to' => $text],
        'lean' => [['task' => $taskId, 'comment' => $id, 'from' => ['omitted' => true], 'to' => $text], ['task' => $taskId, 'comment' => $id, 'omitted' => true]],
    ]];
}

function op_comment_delete(PDO $db, array $req): array
{
    only($req, ['id']);
    $id = id_field($req['id'] ?? null, 'id');
    $comment = comment_out(find_comment($db, $id));
    $replies = (int) one($db, 'SELECT COUNT(*) AS n FROM comments WHERE reply_to = ?', [$id])['n'];
    if ($replies > 0) {
        fail('conflict', "comment #$id has $replies replies: delete them first");
    }
    $db->prepare('DELETE FROM comments WHERE id = ?')->execute([$id]);
    $taskId = $comment['task'];
    $lean = $comment;
    $lean['text'] = ['omitted' => true];
    return [['deleted' => $comment], [
        'action' => 'comment.delete',
        'subject' => "task/$taskId",
        'summary' => "Deleted comment #$id on task #$taskId",
        'payload' => ['task' => $taskId, 'deleted' => $comment],
        'lean' => [['task' => $taskId, 'deleted' => $lean]],
    ]];
}

// ---- events ----------------------------------------------------------------

// emit appends one event line after the commit. A payload too large for one
// line falls back to its lean variants, which say what they left out.
function emit(array $ev): void
{
    $path = getenv('KOMBINAT_EVENTS');
    if (!is_string($path) || $path === '') {
        return;
    }
    $line = '';
    foreach ([$ev['payload'], ...$ev['lean'], ['omitted' => true]] as $payload) {
        $line = json_encode([
            'action' => 'tasks.' . $ev['action'],
            'subject' => $ev['subject'],
            'summary' => cut($ev['summary'], SUMMARY_MAX),
            'payload' => (object) $payload,
        ], JSON_FLAGS);
        if (strlen($line) <= EVENT_LINE_MAX) {
            break;
        }
    }
    try {
        $ok = file_put_contents($path, $line . "\n", FILE_APPEND | LOCK_EX) !== false;
        $why = 'write failed';
    } catch (\Throwable $e) {
        $ok = false;
        $why = $e->getMessage();
    }
    if (!$ok) {
        fail('internal', 'the change is saved, but its event was not written: ' . $why);
    }
}

// cut shortens s to at most max bytes without splitting a UTF-8 character.
function cut(string $s, int $max): string
{
    if (strlen($s) <= $max) {
        return $s;
    }
    $s = substr($s, 0, $max - 3);
    while ($s !== '' && !preg_match('//u', $s)) {
        $s = substr($s, 0, -1);
    }
    return $s . '…';
}
