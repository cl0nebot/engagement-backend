class User < ActiveRecord::Base
  belongs_to :client
  has_many :client_api_keys, foreign_key: :client_id, primary_key: :client_id
  has_many :api_keys, through: :client_api_keys

  enum role: [:user, :vip, :admin]
  after_initialize :set_default_role, :if => :new_record?

  def set_default_role
    self.role ||= :user
  end

  # Include default devise modules. Others available are:
  # :confirmable, :lockable, :timeoutable and :omniauthable
  devise :database_authenticatable, :registerable, :confirmable,
         :recoverable, :rememberable, :trackable, :validatable
end
