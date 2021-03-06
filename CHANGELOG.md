# Change Log
All notable changes to this project will be documented in this file.
This project adheres to [Semantic Versioning](http://semver.org/).

## [UNRELEASED] - 0000-00-00

## [1.4.1] - 2016-03-31
### Fixed
- Fix vm create always erroring and never creating anything

## [1.4.0] - 2016-03-31
### Added
- Allow dynamic ips from bigv
### Fixed
- Fix crash on ipv6 being set from bigv

## [1.3.2] - 2016-02-20
### Fixed
- Fix blocking when a vm create fails

## [1.3.1] - 2016-02-13
### Fixed
- Fix fresh session id never being used

## [1.3.0] - 2016-02-13
### Changed
- Remove all non-alphanumetic password characters, and increase password size to compensate
### Fixed
- Fix login retries with 401 being skipped

## [1.2.1] - 2016-02-05
### Fixed
- Fix provider not setting connection info for provisioners.

## [1.2.0] - 2016-02-03
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
