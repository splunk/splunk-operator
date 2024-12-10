# setup.py

from setuptools import setup, find_packages

setup(
    name='kubectl-splunk',
    version='1.6',  # Update the version accordingly
    description='A kubectl plugin to manage Splunk instances within Kubernetes pods.',
    author='Splunk',
    author_email='support@splunk.com',
    url='https://github.com/splunk/splunk-operator/tools/kubectl-splunk',  
    packages=find_packages(),
    install_requires=[
        'requests',
        'argcomplete',
    ],
    entry_points={
        'console_scripts': [
            'kubectl-splunk=kubectl_splunk.main:main',
        ],
    },
    classifiers=[
        'Programming Language :: Python :: 3',
        'Operating System :: OS Independent',
    ],
    python_requires='>=3.6',
)
