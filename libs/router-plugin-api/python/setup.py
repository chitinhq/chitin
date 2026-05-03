"""Minimal setup for chitin_governance Python lib.

Install for plugin authors:

    pip install -e .

Or vendor directly:

    cp libs/router-plugin-api/python/chitin_governance.py <plugin_dir>/
"""
from setuptools import setup

setup(
    name='chitin-router-plugin-api',
    version='0.0.1',
    py_modules=['chitin_governance'],
    description='Opt-in side-effect gate for chitin router plugin authors.',
    license='MIT',
    python_requires='>=3.10',
)
