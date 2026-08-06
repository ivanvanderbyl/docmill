"""Pull DocLayNet-v1.1 layout annotations without the page images.

Parquet is columnar and the HF filesystem serves byte ranges, so reading only
bboxes/category_id/metadata costs a few MB per shard instead of the 1.1 GB the
image column would add. Output is one JSON line per page, keyed by page_hash —
the same key DocLayNet_extra.zip names its single-page PDFs by.
"""
import json, os, sys, time
os.environ.setdefault('HF_HOME', os.path.dirname(os.path.abspath(__file__)) + '/hf')
from huggingface_hub import HfFileSystem, HfApi
import pyarrow.parquet as pq

REPO = 'docling-project/DocLayNet-v1.1'
out_path = sys.argv[1]

api = HfApi()
files = sorted(f for f in api.list_repo_files(REPO, repo_type='dataset') if f.endswith('.parquet'))
fs = HfFileSystem()
written = 0
with open(out_path, 'w') as sink:
    for i, name in enumerate(files):
        split = os.path.basename(name).split('-')[0]
        t0 = time.time()
        with fs.open(f'datasets/{REPO}/{name}', 'rb') as fh:
            table = pq.ParquetFile(fh).read(columns=['bboxes', 'category_id', 'metadata'])
        for row in range(table.num_rows):
            meta = table['metadata'][row].as_py()
            sink.write(json.dumps({
                'hash': meta['page_hash'],
                'split': split,
                'cat': meta['doc_category'],
                'doc': meta['original_filename'],
                'page_no': meta['page_no'],
                'w': meta['original_width'],
                'h': meta['original_height'],
                'cw': meta['coco_width'],
                'ch': meta['coco_height'],
                'boxes': [[round(v, 2) for v in b] for b in table['bboxes'][row].as_py()],
                'cats': table['category_id'][row].as_py(),
            }) + '\n')
            written += 1
        print(f"[{i+1}/{len(files)}] {os.path.basename(name)} {table.num_rows} rows in {time.time()-t0:.1f}s (total {written})", flush=True)
print("DONE", written)
