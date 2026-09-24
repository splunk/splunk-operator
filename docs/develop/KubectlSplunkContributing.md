---
title: Contributing to kubectl-splunk
parent: Develop & Contribute
nav_order: 8
---

# Contributing to kubectl-splunk

Contributions to the [kubectl-splunk plugin](../platforms/KubectlSplunk.md) are welcome! Please submit issues and pull requests via the project's GitHub repository.

## How to Contribute

1. **Fork the Repository**: Click the "Fork" button on the GitHub repository page to create your own copy

2. **Clone Your Fork**:

   ```bash
   git clone https://github.com/splunk/splunk-operator.git
   cd tools/kubectl-splunk
   ```

3. **Create a Feature Branch**:

   ```bash
   git checkout -b feature/your-feature-name
   ```

4. **Make Your Changes**: Implement your feature or bug fix. Ensure your code follows the project's coding standards

5. **Run Tests**: Ensure all tests pass before committing

   ```bash
   python -m unittest discover -s tests
   ```

6. **Commit Your Changes**:

   ```bash
   git commit -m "Add feature X to kubectl-splunk"
   ```

7. **Push to Your Fork**:

   ```bash
   git push origin feature/your-feature-name
   ```

8. **Open a Pull Request**: Navigate to the original repository and open a pull request describing your changes

## Coding Standards

- Follow PEP 8 for Python code style
- Write meaningful docstrings for modules, classes, and functions
- Ensure that your code is well-documented and maintainable

## Reporting Issues

If you encounter any issues or bugs, please open an issue on the [GitHub Issues](https://github.com/splunk/splunk-operator/issues) page. Provide detailed information to help us understand and resolve the problem.
