<?php
// Memory 5 API entrypoint: reads one JSON request on stdin, answers one JSON document on stdout.
// Mesh: POST /api/modules/memory5/run {"entrypoint":"api","stdin":"{\"action\":\"query\",\"q\":\"...\"}"}
//       or: kombinat module run --entrypoint api --stdin memory5 < request.json
// Answer: {"ok":true,"data":{...}} or {"ok":false,"error":"..."}; exit 0, 1 (runtime) or 2 (bad request).
declare(strict_types=1);

namespace Memory5;

require __DIR__ . '/engine.php';

/** Actions this entrypoint answers; each maps to the same operation the CLI runs. */
const ACTIONS = ['query', 'status', 'index', 'memory.add', 'memory.list', 'memory.delete', 'journal.add', 'journal.list'];

function field(array $req, string $key, string $type, mixed $default = null): mixed
{
    if (!array_key_exists($key, $req)) {
        if ($default === null) {
            fail("field \"$key\" is required", 2);
        }
        return $default;
    }
    $v = $req[$key];
    $ok = match ($type) {
        'string' => is_string($v),
        'int' => is_int($v) && $v > 0,
        'bool' => is_bool($v),
    };
    if (!$ok) {
        fail("field \"$key\" must be " . ($type === 'int' ? 'a positive integer' : "a $type"), 2);
    }
    return $v;
}

function handle(array $req): array
{
    $action = $req['action'] ?? null;
    if (!is_string($action) || !in_array($action, ACTIONS, true)) {
        fail('field "action" must be one of: ' . implode(', ', ACTIONS), 2);
    }
    $allowed = match ($action) {
        'query' => ['q', 'limit', 'source'],
        'index' => ['rebuild'],
        'memory.add', 'journal.add' => ['text'],
        'memory.list' => ['limit'],
        'memory.delete' => ['id'],
        'journal.list' => ['day', 'limit'],
        default => [],
    };
    foreach (array_keys($req) as $k) {
        if ($k !== 'action' && !in_array($k, $allowed, true)) {
            fail("unknown field \"$k\" for action $action", 2);
        }
    }
    $x = new Ctx();
    return match ($action) {
        'query' => op_query($x, field($req, 'q', 'string'), array_key_exists('limit', $req) ? field($req, 'limit', 'int') : null,
            field($req, 'source', 'string', '')),
        'status' => op_status($x),
        'index' => op_index($x, field($req, 'rebuild', 'bool', false)),
        'memory.add' => op_memory_add($x, field($req, 'text', 'string')),
        'memory.list' => op_memory_list($x, field($req, 'limit', 'int', 50)),
        'memory.delete' => op_memory_delete($x, field($req, 'id', 'int')),
        'journal.add' => op_journal_add($x, field($req, 'text', 'string')),
        'journal.list' => op_journal_list($x, field($req, 'day', 'string', ''), field($req, 'limit', 'int', 50)),
    };
}

strict_errors();
try {
    $raw = (string) stream_get_contents(STDIN);
    $req = json_decode($raw, true);
    if (!is_array($req) || array_is_list($req) && $req !== []) {
        fail('stdin must hold one JSON object, e.g. {"action":"query","q":"..."}', 2);
    }
    echo json_out(['ok' => true, 'data' => handle($req)]);
    exit(0);
} catch (Fail $e) {
    echo json_out(['ok' => false, 'error' => $e->getMessage()]);
    exit($e->getCode() ?: 1);
} catch (\Throwable $e) {
    echo json_out(['ok' => false, 'error' => 'internal error: ' . $e->getMessage()]);
    exit(1);
}
