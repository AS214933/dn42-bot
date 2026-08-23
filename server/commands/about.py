from base import bot
from telebot.types import ReplyKeyboardRemove
from version import REPOSITORY, UPSTREAM, get_build_date, get_git_commit


@bot.message_handler(commands=["about"])
def show_about(message):
    build_date = get_build_date()
    git_commit = get_git_commit()

    text = (
        f"*Build Info*\n"
        f"Build Date: `{build_date}`\n"
        f"Git Commit: `{git_commit}`\n"
        f"\n"
        f"*Source Code*\n"
        f"Repository: {REPOSITORY}\n"
        f"Upstream: {UPSTREAM}"
    )
    bot.send_message(
        message.chat.id,
        text,
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )
