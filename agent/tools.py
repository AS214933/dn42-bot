import asyncio
import shlex
import subprocess

import base
import sentry_sdk


def simple_run(command, timeout=3):
    try:
        output = (
            subprocess.check_output(shlex.split(command), timeout=timeout, stderr=subprocess.STDOUT)
            .decode("utf-8")
            .strip()
        )
    except subprocess.CalledProcessError as e:
        output = e.output.decode("utf-8").strip()
    return output


async def async_simple_run(command, timeout=3):
    """Non-blocking wrapper: runs simple_run in a thread to avoid blocking the event loop."""
    return await asyncio.to_thread(simple_run, command, timeout=timeout)


def set_sentry(func):
    async def wrapper(request):
        if base.SENTRY_DSN:
            with sentry_sdk.start_transaction(name=f"Agent {request.rel_url}", sampled=True) as transaction:
                transaction.set_tag("url", request.rel_url)
                try:
                    ret = await func(request)
                    transaction.set_http_status(ret.status)
                    return ret
                except Exception as e:
                    transaction.set_http_status(500)
                    sentry_sdk.capture_exception(e)
                    raise
        else:
            return await func(request)

    return wrapper
