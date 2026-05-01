import { NativeConnection, Worker } from '@temporalio/worker';
import * as activities from './activity.ts';

const TASK_QUEUE = 'chitin-worker-q';

async function main() {
  const connection = await NativeConnection.connect({ address: '127.0.0.1:7233' });
  const worker = await Worker.create({
    connection,
    namespace: 'default',
    taskQueue: TASK_QUEUE,
    workflowsPath: new URL('./workflow.ts', import.meta.url).pathname,
    activities,
  });
  console.log(`[temporal-worker] connected; taskQueue=${TASK_QUEUE} namespace=default`);
  await worker.run();
}

main().catch((err) => {
  console.error('[temporal-worker] fatal:', err);
  process.exit(1);
});
