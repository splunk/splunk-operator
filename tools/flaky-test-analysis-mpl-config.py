"""Matplotlib customizations for flaky-test-analysis heatmaps.

Monkey-patches matplotlib to:
- Increase font sizes for readability
- Wrap long y-axis tick labels
- Override the library's hardcoded title fontsize
"""
import textwrap
import matplotlib as mpl
import matplotlib.pyplot as plt

WRAP_WIDTH = 60
TITLE_FONTSIZE = 64

mpl.rcParams.update({
    "font.size": 50,
    "xtick.labelsize": 40,
    "ytick.labelsize": 40,
    "axes.labelsize": 60,
})

_original_savefig = plt.savefig
_original_title = plt.title


def _title_with_fontsize(*args, **kwargs):
    kwargs["fontsize"] = TITLE_FONTSIZE
    return _original_title(*args, **kwargs)


def _savefig_with_wrapped_labels(*args, **kwargs):
    fig = plt.gcf()
    for ax in fig.axes:
        labels = ax.get_yticklabels()
        if labels:
            ticks = ax.get_yticks()
            ax.set_yticks(ticks)
            ax.set_yticklabels([textwrap.fill(l.get_text(), WRAP_WIDTH) for l in labels])
    _original_savefig(*args, **kwargs)


plt.title = _title_with_fontsize
plt.savefig = _savefig_with_wrapped_labels
