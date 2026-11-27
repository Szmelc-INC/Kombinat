<?php
// Tasks CLI entrypoint: turns argv into the same request the api entrypoint
// reads from stdin, runs it through the same handler, and prints text
// (or, with --json, the api entrypoint's JSON document). `tasks help` lists the verbs.
// Inside the mesh: kombinat module run tasks -- VERB ARGS...
// Standalone: KOMBINAT_DATA=DIR php -f src/cli.php -- VERB ARGS...
declare(strict_types=1);

namespace Kombinat\Tasks;

require __DIR__ . '/tasks.php';

strict();

// Each verb: its op, positional fields in order, and the options it takes.
const VERBS = [
    'add' => ['add', ['title'], ['status', 'priority', 'owner', 'parent', 'body']],
    'list' => ['list', [], ['status', 'owner', 'parent', 'limit']],
    'show' => ['show', ['id'], []],
    'update' => ['update', ['id'], ['title', 'status', 'priority', 'owner', 'parent', 'body']],
    'delete' => ['delete', ['id'], []],
    'comment add' => ['comment.add', ['task', 'text'], ['reply-to', 'author']],
    'comment edit' => ['comment.edit', ['id', 'text'], []],
    'comment delete' => ['comment.delete', ['id'], []],
];

const USAGE = <<<'TXT'
Usage: tasks [--json] VERB ARGS...
  add TITLE [--status S] [--priority N] [--owner NAME] [--parent ID] [--body JSON|-]
  list [--status S] [--owner NAME] [--parent ID|none] [--limit N]
  show ID
  update ID [--title T] [--status S] [--priority N] [--owner NAME|""] [--parent ID|none] [--body JSON|-]
  delete ID
  comment add TASK TEXT|- [--reply-to COMMENT] [--author NAME]
  comment edit COMMENT TEXT|-
  comment delete COMMENT
  config      print the effective configuration as JSON
  help        print this help
States: idea, planned, queued, running, done, failed, dropped. "-" reads the value from stdin.
Exit codes: 0 ok, 1 internal, 2 invalid, 3 not found, 4 conflict, 5 config.

TXT;

function cli_main(array $argv): int
{
    $json = false;
    $words = [];
    $opts = [];
    $args = array_slice($argv, 1);
    $plain = false;
    for ($i = 0; $i < count($args); $i++) {
        $a = $args[$i];
        if (!$plain && $a === '--') {
            $plain = true;
        } elseif (!$plain && $a === '--json') {
            $json = true;
        } elseif (!$plain && ($a === '-h' || $a === '--help')) {
            fwrite(STDOUT, USAGE);
            return 0;
        } elseif (!$plain && str_starts_with($a, '--')) {
            $name = substr($a, 2);
            if (str_contains($name, '=')) {
                [$name, $value] = explode('=', $name, 2);
            } elseif ($i + 1 < count($args)) {
                $value = $args[++$i];
            } else {
                return usage_error("--$name needs a value");
            }
            if (array_key_exists($name, $opts)) {
                return usage_error("--$name given twice");
            }
            $opts[$name] = $value;
        } else {
            $words[] = $a;
        }
    }
    if (!$words) {
        return usage_error('no verb given');
    }
    if ($words[0] === 'help') {
        fwrite(STDOUT, USAGE);
        return 0;
    }
    if ($words[0] === 'config') {
        if (count($words) > 1 || $opts) {
            return usage_error('config takes no arguments');
        }
        try {
            fwrite(STDOUT, json_encode(config(), JSON_FLAGS | JSON_PRETTY_PRINT) . "\n");
            return 0;
        } catch (Failure $e) {
            fwrite(STDERR, 'tasks: ' . $e->getMessage() . "\n");
            return EXIT_CODES[$e->kind];
        }
    }
    $verb = $words[0] === 'comment' ? 'comment ' . ($words[1] ?? '') : $words[0];
    if (!isset(VERBS[$verb])) {
        return usage_error("unknown verb: $verb");
    }
    [$op, $positional, $allowed] = VERBS[$verb];
    $values = array_slice($words, $words[0] === 'comment' ? 2 : 1);
    if (count($values) !== count($positional)) {
        return usage_error("$verb takes " . (count($positional) ? implode(', ', $positional) : 'no arguments'));
    }
    foreach (array_keys($opts) as $name) {
        if (!in_array($name, $allowed, true)) {
            return usage_error("$verb does not take --$name");
        }
    }
    try {
        $req = ['op' => $op] + request(array_combine($positional, $values), $opts);
        [$doc, $code] = respond($req);
    } catch (Failure $e) {
        [$doc, $code] = [['ok' => false, 'op' => $op, 'error' => ['code' => $e->kind, 'message' => $e->getMessage()]], EXIT_CODES[$e->kind]];
    }
    if ($json) {
        fwrite(STDOUT, json_encode($doc, JSON_FLAGS | JSON_INVALID_UTF8_SUBSTITUTE) . "\n");
    } elseif ($doc['ok']) {
        fwrite(STDOUT, render($op, $doc['data']));
    } else {
        fwrite(STDERR, 'tasks: ' . $doc['error']['message'] . "\n");
    }
    return $code;
}

function usage_error(string $why): int
{
    fwrite(STDERR, "tasks: $why\n" . USAGE);
    return EXIT_CODES['invalid'];
}

// request turns positional values and options into typed request fields.
function request(array $positional, array $opts): array
{
    $stdinUsed = false;
    $stdin = static function (string $v) use (&$stdinUsed): string {
        if ($v !== '-') {
            return $v;
        }
        if ($stdinUsed) {
            fail('invalid', 'only one value can come from stdin');
        }
        $stdinUsed = true;
        $in = stream_get_contents(STDIN);
        return is_string($in) ? $in : '';
    };
    $int = static fn (string $v): int|string => preg_match('/^-?[0-9]{1,18}$/', $v) ? (int) $v : $v;
    $req = [];
    foreach ($positional as $k => $v) {
        $req[$k] = $k === 'text' ? $stdin($v) : ($k === 'title' ? $v : $int($v));
    }
    foreach ($opts as $k => $v) {
        $req[str_replace('-', '_', $k)] = match ($k) {
            'priority', 'limit', 'reply-to' => $int($v),
            'parent' => $v === 'none' ? null : $int($v),
            'body' => body($stdin($v)),
            default => $v,
        };
    }
    return $req;
}

function body(string $raw): mixed
{
    try {
        return json_decode($raw, false, 64, JSON_THROW_ON_ERROR);
    } catch (\JsonException $e) {
        fail('invalid', 'body must be a JSON object: ' . $e->getMessage());
    }
}

// ---- text output -------------------------------------------------------------

function render(string $op, array $data): string
{
    switch ($op) {
        case 'add':
            $t = $data['task'];
            return "Added task #{$t['id']} ({$t['status']}): {$t['title']}\n";
        case 'list':
            return table($data['tasks']);
        case 'show':
            return show($data);
        case 'update':
            $changes = (array) $data['changes'];
            $id = $data['task']['id'];
            return $changes ? "Updated task #$id: " . describe($changes) . "\n" : "Task #$id unchanged.\n";
        case 'delete':
            $t = $data['deleted'];
            return "Deleted task #{$t['id']}: {$t['title']} (with {$data['comments']} comments)\n";
        case 'comment.add':
            $c = $data['comment'];
            $reply = $c['reply_to'] === null ? '' : " in reply to #{$c['reply_to']}";
            return "Added comment #{$c['id']} to task #{$c['task']}$reply\n";
        case 'comment.edit':
            $c = $data['comment'];
            return $data['changed'] ? "Edited comment #{$c['id']} on task #{$c['task']}\n" : "Comment #{$c['id']} unchanged.\n";
        case 'comment.delete':
            $c = $data['deleted'];
            return "Deleted comment #{$c['id']} from task #{$c['task']}\n";
    }
    return '';
}

function table(array $tasks): string
{
    if (!$tasks) {
        return "No tasks.\n";
    }
    $rows = [['ID', 'STATUS', 'PRI', 'OWNER', 'PARENT', 'COMMENTS', 'TITLE']];
    foreach ($tasks as $t) {
        $rows[] = ['#' . $t['id'], $t['status'], (string) $t['priority'], $t['owner'] ?: '—', $t['parent'] === null ? '—' : '#' . $t['parent'], (string) $t['comments'], $t['title']];
    }
    $widths = [];
    foreach ($rows as $r) {
        foreach ($r as $i => $cell) {
            $widths[$i] = max($widths[$i] ?? 0, width($cell));
        }
    }
    $out = '';
    foreach ($rows as $r) {
        $cells = [];
        foreach ($r as $i => $cell) {
            $cells[] = $i === count($r) - 1 ? $cell : $cell . str_repeat(' ', $widths[$i] - width($cell));
        }
        $out .= implode('  ', $cells) . "\n";
    }
    return $out;
}

// width counts characters, not bytes, so "—" pads like one column.
function width(string $s): int
{
    return (int) preg_match_all('/./su', $s);
}

function show(array $data): string
{
    $t = $data['task'];
    $dash = static fn (mixed $v): string => $v === null || $v === '' ? '—' : (string) $v;
    $out = "Task #{$t['id']}: {$t['title']}\n"
        . "status: {$t['status']} · priority: {$t['priority']} · owner: " . $dash($t['owner'])
        . ' · parent: ' . ($t['parent'] === null ? '—' : '#' . $t['parent']) . "\n"
        . "created: {$t['created_at']} · updated: {$t['updated_at']}\n";
    $body = json_encode($t['body'], JSON_FLAGS);
    if ($body !== '{}') {
        $out .= "body: $body\n";
    }
    if ($data['children']) {
        $out .= "children:\n";
        foreach ($data['children'] as $c) {
            $out .= "  #{$c['id']} ({$c['status']}) {$c['title']}\n";
        }
    }
    $out .= $data['comments'] ? "comments:\n" . comments($data['comments'], 1) : "comments: none\n";
    return $out;
}

function comments(array $list, int $depth): string
{
    $out = '';
    $pad = str_repeat('  ', $depth);
    foreach ($list as $c) {
        $lines = explode("\n", $c['text']);
        $out .= "$pad#{$c['id']} {$c['author']}, {$c['created_at']}: " . array_shift($lines) . "\n";
        foreach ($lines as $line) {
            $out .= "$pad  $line\n";
        }
        $out .= comments($c['replies'], $depth + 1);
    }
    return $out;
}

exit(cli_main($argv));
