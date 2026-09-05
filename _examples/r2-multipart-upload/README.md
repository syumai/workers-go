# R2 Multipart Upload Example

This example demonstrates how to use the R2 multipart upload API with Cloudflare Workers.

## Features

- Initiate multipart uploads
- Upload individual parts
- Complete multipart uploads
- Abort multipart uploads
- Regular PUT/GET operations for comparison

## API Endpoints

### Multipart Upload Endpoints

1. **Initiate Multipart Upload**
   ```
   POST /multipart/initiate?key=<object-key>
   ```
   Returns:
   ```json
   {
     "uploadId": "string",
     "key": "string"
   }
   ```

2. **Upload Part**
   ```
   PUT /multipart/upload?key=<object-key>&uploadId=<upload-id>&partNumber=<part-number>
   Body: <part-data>
   ```
   Returns:
   ```json
   {
     "partNumber": 1,
     "etag": "string"
   }
   ```

3. **Complete Multipart Upload**
   ```
   POST /multipart/complete?key=<object-key>&uploadId=<upload-id>
   Body: {
     "parts": [
       {
         "partNumber": 1,
         "etag": "string"
       }
     ]
   }
   ```
   Returns: Object metadata

4. **Abort Multipart Upload**
   ```
   DELETE /multipart/abort?key=<object-key>&uploadId=<upload-id>
   ```

### Regular Object Operations

1. **Upload Object**
   ```
   PUT /<object-key>
   Body: <file-data>
   ```

2. **Download Object**
   ```
   GET /<object-key>
   ```

## Usage Example

### Using curl for multipart upload:

```bash
# 1. Initiate multipart upload
RESPONSE=$(curl -X POST "https://your-worker.workers.dev/multipart/initiate?key=large-file.bin")
UPLOAD_ID=$(echo $RESPONSE | jq -r '.uploadId')

# 2. Upload parts (split your file into parts first)
# For example, split a file into 10MB parts:
split -b 10m large-file.bin part-

# Upload each part
curl -X PUT \
  --data-binary @part-aa \
  "https://your-worker.workers.dev/multipart/upload?key=large-file.bin&uploadId=$UPLOAD_ID&partNumber=1" \
  > part1.json

curl -X PUT \
  --data-binary @part-ab \
  "https://your-worker.workers.dev/multipart/upload?key=large-file.bin&uploadId=$UPLOAD_ID&partNumber=2" \
  > part2.json

# 3. Complete the upload
PARTS=$(jq -s '[.[] | {partNumber: .partNumber, etag: .etag}]' part*.json)
curl -X POST \
  -H "Content-Type: application/json" \
  --data "{\"parts\": $PARTS}" \
  "https://your-worker.workers.dev/multipart/complete?key=large-file.bin&uploadId=$UPLOAD_ID"

# 4. Download the uploaded file
curl "https://your-worker.workers.dev/large-file.bin" -o downloaded-file.bin
```

### Using JavaScript:

```javascript
// Example multipart upload client
async function multipartUpload(url, key, file) {
  const PART_SIZE = 10 * 1024 * 1024; // 10MB
  
  // 1. Initiate multipart upload
  const initResponse = await fetch(`${url}/multipart/initiate?key=${key}`, {
    method: 'POST'
  });
  const { uploadId } = await initResponse.json();
  
  // 2. Upload parts
  const parts = [];
  const totalParts = Math.ceil(file.size / PART_SIZE);
  
  for (let i = 0; i < totalParts; i++) {
    const start = i * PART_SIZE;
    const end = Math.min(start + PART_SIZE, file.size);
    const part = file.slice(start, end);
    
    const partResponse = await fetch(
      `${url}/multipart/upload?key=${key}&uploadId=${uploadId}&partNumber=${i + 1}`,
      {
        method: 'PUT',
        body: part
      }
    );
    
    const partData = await partResponse.json();
    parts.push(partData);
    
    console.log(`Uploaded part ${i + 1}/${totalParts}`);
  }
  
  // 3. Complete upload
  const completeResponse = await fetch(
    `${url}/multipart/complete?key=${key}&uploadId=${uploadId}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ parts })
    }
  );
  
  return await completeResponse.json();
}
```

## Setup

1. Copy this example directory
2. Update `wrangler.toml` with your R2 bucket configuration
3. Install dependencies: `go mod tidy`
4. Deploy: `make deploy`

## Notes

- Minimum part size is 5MB (except for the last part)
- Multipart uploads are automatically aborted after 7 days if not completed
- Parts can be uploaded in parallel for better performance
- Each part receives an ETag that must be provided when completing the upload
