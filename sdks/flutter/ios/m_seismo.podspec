Pod::Spec.new do |s|
  s.name             = 'm_seismo'
  s.version          = '0.1.0'
  s.summary          = 'iOS-first Flutter bridge for the native m-Seismo SDK.'
  s.description      = <<-DESC
A thin Flutter bridge over the native m-Seismo iOS SDK. The native Swift
sources are compiled directly into this plugin pod; host apps must not link
the native MSeismo package separately.
                       DESC
  s.homepage         = 'https://github.com/MicroClub-USTHB/m-Seismo'
  s.license          = { :file => '../LICENSE' }
  s.author           = { 'MicroClub' => 'microclub@usthb.dz' }
  s.source           = { :path => '.' }

  s.source_files = 'Classes/**/*.{swift,h}',
                   '../../ios/Sources/MSeismo/**/*.swift'
  s.resource_bundles = {
    'm_seismo_privacy' => ['Resources/PrivacyInfo.xcprivacy']
  }

  s.dependency 'Flutter'
  s.frameworks = 'SwiftUI', 'UIKit', 'Network', 'Combine', 'CoreMotion'
  s.platform = :ios, '13.0'
  s.swift_version = '5.7'

  # Flutter.framework does not contain an i386 slice.
  s.pod_target_xcconfig = {
    'DEFINES_MODULE' => 'YES',
    'EXCLUDED_ARCHS[sdk=iphonesimulator*]' => 'i386'
  }
end
