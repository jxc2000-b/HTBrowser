# HTBrowser
> [!IMPORTANT]
Recommended to use this with self hosted model but thats not implemented (yet!) so either fork and implement yourself, use the provided test data or test with personal data at your risk.

HTBrowser is a self hosted health data dashboard/viewer powered by GenAI. 
Simply enter you data in markdown format and store in a /data subdirectory or use the provided test data to demo.

To run: 
```
cd backend
DATA_DIR=test-data go run ./pkg/cmd/server
```

In another shell 
```
cd frontend
pnpm run dev
```
## What about hallucination? 
A validator double checks every value produced by the LLM against the references in the source document. If a mismatch is detected it command a re-generation of the page, if hallucination is detected a second time it highlights and strikes through those values. 

## Why Markdown?
Plaintext works just fine but markdown is extremely close to plain text with some extra features that make it easier for LLMs to comprehend. Plain-text is kind of a universal standard and opens the door for pre-processing.

