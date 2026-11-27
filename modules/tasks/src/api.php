<?php
// Tasks api entrypoint: reads one JSON request on stdin, for example
// {"op":"add","title":"Write docs"}, and answers one JSON document on stdout:
// {"ok":true,"op":"add","data":{...}} or {"ok":false,"op":"add","error":{"code":"...","message":"..."}}.
// The exit code follows the error code (MODULE.md). It takes no arguments.
// Inside the mesh: POST /api/modules/tasks/run {"entrypoint":"api","stdin":"..."}.
// Standalone: KOMBINAT_DATA=DIR php -f src/api.php < request.json
declare(strict_types=1);

namespace Kombinat\Tasks;

require __DIR__ . '/tasks.php';

strict();

function api_main(array $argv): int
{
    $raw = stream_get_contents(STDIN);
    $req = null;
    $problem = null;
    if (count($argv) > 1) {
        $problem = 'the api entrypoint takes no arguments; send the request as JSON on stdin';
    } elseif (!is_string($raw) || trim($raw) === '') {
        $problem = 'the api entrypoint reads one JSON request on stdin, for example {"op":"list"}';
    } else {
        try {
            $req = json_decode($raw, false, 64, JSON_THROW_ON_ERROR);
        } catch (\JsonException $e) {
            $problem = 'the request is not JSON: ' . $e->getMessage();
        }
        if ($problem === null && !$req instanceof \stdClass) {
            $problem = 'the request must be a JSON object';
        }
    }
    if ($problem !== null) {
        $doc = ['ok' => false, 'op' => null, 'error' => ['code' => 'invalid', 'message' => $problem]];
        $code = EXIT_CODES['invalid'];
    } else {
        [$doc, $code] = respond(get_object_vars($req));
    }
    try {
        $out = json_encode($doc, JSON_FLAGS | JSON_INVALID_UTF8_SUBSTITUTE);
    } catch (\JsonException $e) {
        // Still exactly one document: the answer could not be encoded.
        $code = EXIT_CODES['internal'];
        $out = json_encode(['ok' => false, 'op' => $doc['op'] ?? null, 'error' => ['code' => 'internal', 'message' => 'the answer could not be encoded: ' . $e->getMessage()]], JSON_FLAGS | JSON_INVALID_UTF8_SUBSTITUTE);
    }
    fwrite(STDOUT, $out . "\n");
    return $code;
}

exit(api_main($argv));
