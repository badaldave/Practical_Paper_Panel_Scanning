import torch
import time
import logging
import sys
from datetime import datetime, timedelta
import psycopg
from app.config import Config
from app.db.repository import WorkerRepository
from app.pipeline.engine import ProcessingEngine

# Set up logging configuration
logging.basicConfig(
	level=logging.INFO,
	format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
	handlers=[logging.StreamHandler(sys.stdout)]
)
logger = logging.getLogger("OCRWorkerDaemon")

def main():
	logger.info(f"Starting OCR Worker Daemon (Worker ID: {Config.WORKER_ID})")
	
	try:
		# Initialize the AI pipeline engine
		engine = ProcessingEngine()
		logger.info("Processing engine initialized successfully. Entering poll loop...")
	except Exception as e:
		logger.critical(f"Failed to initialize processing engine: {e}")
		sys.exit(1)

	poll_interval = Config.POLL_INTERVAL

	# Self-healing: on startup, reclaim any jobs a previous (crashed) worker left
	# stuck in 'processing' so they don't sit there forever.
	try:
		reclaimed = WorkerRepository.reap_stale_jobs()
		if reclaimed:
			logger.warning(f"Startup reaper reclaimed {reclaimed} stale job(s) left in 'processing'.")
	except Exception as reap_err:
		logger.error(f"Startup reaper failed: {reap_err}")

	last_reap = time.monotonic()

	while True:
		try:
			# Periodically reclaim stale jobs (e.g. a sibling worker died mid-job).
			if time.monotonic() - last_reap >= Config.REAP_INTERVAL_SECONDS:
				try:
					reclaimed = WorkerRepository.reap_stale_jobs()
					if reclaimed:
						logger.warning(f"Reaper reclaimed {reclaimed} stale job(s) left in 'processing'.")
				except Exception as reap_err:
					logger.error(f"Reaper failed: {reap_err}")
				last_reap = time.monotonic()

			# Poll for a job
			job = WorkerRepository.dequeue_job()
			
			if not job:
				# No jobs to process, sleep
				time.sleep(poll_interval)
				continue
			
			job_id = job["id"]
			doc_id = job["document_id"]
			tenant_id = job["tenant_id"]
			attempts = job["attempts"]
			max_attempts = job["max_attempts"]

			logger.info(f"Dequeued job {job_id} for Document {doc_id} (Attempt {attempts}/{max_attempts})")

			# Record job attempt
			attempt_id = WorkerRepository.record_job_attempt(job_id, attempts)
			
			try:
				# Run processing pipeline
				start_time = datetime.utcnow()
				engine.process_document(doc_id, tenant_id)
				
				# Update status
				WorkerRepository.complete_job(job_id, doc_id)
				WorkerRepository.update_job_attempt(attempt_id, "completed")
				
				duration = (datetime.utcnow() - start_time).total_seconds()
				logger.info(f"Job {job_id} completed successfully in {duration:.2f} seconds.")
				
			except Exception as pipeline_err:
				err_msg = str(pipeline_err)
				logger.error(f"Error executing processing pipeline for job {job_id}: {err_msg}")
				
				# Update attempt status
				WorkerRepository.update_job_attempt(attempt_id, "failed", err_msg)
				
				if attempts < max_attempts:
					# Queue for retry with 30s backoff multiplier
					next_run = datetime.utcnow() + timedelta(seconds=30 * attempts)
					WorkerRepository.fail_job(job_id, doc_id, f"Retry queued: {err_msg}")
					# Re-queue job back to pending status for next retry run
					with psycopg.connect(Config.DATABASE_URL) as conn:
						with conn.cursor() as cur:
							cur.execute(
								"UPDATE processing_jobs SET status = 'retrying', error_message = %s, run_at = %s, locked_at = NULL, locked_by = NULL WHERE id = %s",
								(err_msg, next_run, job_id)
							)
							conn.commit()
					logger.info(f"Job {job_id} set to retry at {next_run}")
				else:
					# Max attempts exceeded, fail job permanently
					WorkerRepository.fail_job(job_id, doc_id, f"Max attempts reached. Error: {err_msg}")
					logger.error(f"Job {job_id} failed permanently (Max attempts exceeded).")

		except KeyboardInterrupt:
			logger.info("Gracefully stopping worker daemon...")
			break
		except Exception as loop_err:
			logger.error(f"Error in main polling loop: {loop_err}")
			time.sleep(poll_interval)

if __name__ == "__main__":
	main()
