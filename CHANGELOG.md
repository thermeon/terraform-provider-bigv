# Change Log
All notable changes to this project will be documented in this file.
This project adheres to [Semantic Versioning](http://semver.org/).

## [UNRELEASED] - 0000-00-00
### Added
- firstboot_script attribute for bootstrapping
### Changed
- Increasse VM provisioning timeout to 20 minutes to allow for firstboot scripts
- Ignore most ssh errors, but log them. Often we get errors as ssh comes up for the first time

## [1.1.0] - 2016-02-01
### Changed
- Synchronous creation now only applies to the initial request to vm_create, which should increase parellism
### Added
- Creation now waits for the VM to be imaged fully
- Partial state is now supported for creation
### Fixed
- Root password being lost after vm create

## [1.0.0] - 2016-01-31
### Added
- Initial full release of bigv provider
